use anyhow::{anyhow, Result};
use axum::{
    extract::{
        ws::{Message as WsMessage, WebSocket, WebSocketUpgrade},
        Path, State,
    },
    response::{Html, IntoResponse},
    routing::{get, post},
    Json, Router,
};
use iroh::{protocol::Router as IrohRouter, Endpoint, EndpointId};
use iroh_gossip::{api::Event as GossipEvent, net::Gossip, proto::TopicId};
use iroh_ping::Ping;
use iroh_tickets::{endpoint::EndpointTicket, Ticket};
use n0_future::StreamExt;
use serde::{Deserialize, Serialize};
use std::{
    collections::HashMap,
    env,
    net::SocketAddr,
    sync::{Arc, Mutex},
    time::{SystemTime, UNIX_EPOCH},
};
use tokio::sync::broadcast;
use tokio::time::{sleep, Duration};

#[derive(Clone)]
struct IndexState {
    entries: Arc<Mutex<HashMap<String, Entry>>>,
    updates_tx: broadcast::Sender<String>,
}

#[derive(Clone, Serialize, Deserialize)]
struct Entry {
    node: String,
    ticket: String,
    updated_at_unix: u64,
}

#[derive(Serialize, Deserialize)]
struct RegisterRequest {
    node: String,
    ticket: String,
}

#[derive(Serialize, Deserialize)]
struct RegisterResponse {
    ok: bool,
    entry: Entry,
}

#[derive(Serialize, Deserialize)]
struct ListResponse {
    nodes: Vec<Entry>,
}

#[derive(Serialize, Deserialize)]
struct PutNodesRequest {
    nodes: Vec<PutNode>,
}

#[derive(Serialize, Deserialize)]
struct PutNode {
    node: String,
    ticket: String,
}

#[derive(Serialize, Deserialize)]
struct GossipHeartbeat {
    kind: String,
    node: String,
    endpoint_id: String,
    unix: u64,
}

const GOSSIP_TOPIC: TopicId = TopicId::from_bytes(*b"mesh-v3-index-dialtone-heartbeat");

const INDEX_HTML: &str = include_str!("index.html");

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

fn parse_ticket(s: &str) -> Result<EndpointTicket> {
    <EndpointTicket as Ticket>::deserialize(s).map_err(|e| anyhow!("failed to parse ticket: {e}"))
}

fn default_node_name(fallback_id: &str) -> String {
    if let Ok(v) = env::var("MESH_V3_NODE") {
        let t = v.trim();
        if !t.is_empty() {
            return t.to_string();
        }
    }
    if let Ok(v) = env::var("HOSTNAME") {
        let t = v.trim();
        if !t.is_empty() {
            return t.to_string();
        }
    }
    fallback_id.to_string()
}

fn default_index_url() -> String {
    env::var("MESH_V3_INDEX_URL")
        .map(|v| v.trim().to_string())
        .ok()
        .filter(|v| !v.is_empty())
        .unwrap_or_else(|| "https://index.dialtone.earth".to_string())
}

fn auto_register_enabled() -> bool {
    env::var("MESH_V3_NO_AUTO_REGISTER").is_err()
}

fn spawn_auto_register(node: String, ticket: String, index_url: String) {
    tokio::spawn(async move {
        let client = reqwest::Client::new();
        let url = format!("{}/register", normalize_index(&index_url));
        loop {
            let req = RegisterRequest {
                node: node.clone(),
                ticket: ticket.clone(),
            };
            let res = client.post(&url).json(&req).send().await;
            match res {
                Ok(resp) if resp.status().is_success() => {
                    eprintln!("auto-register ok node={} index={}", node, index_url);
                }
                Ok(resp) => {
                    let status = resp.status();
                    let body = resp.text().await.unwrap_or_default();
                    eprintln!(
                        "auto-register failed node={} status={} body={}",
                        node, status, body
                    );
                }
                Err(err) => {
                    eprintln!("auto-register error node={} err={}", node, err);
                }
            }
            sleep(Duration::from_secs(60)).await;
        }
    });
}

fn ticket_endpoint_id(ticket: &EndpointTicket) -> EndpointId {
    ticket.endpoint_addr().node_id
}

async fn fetch_nodes(index_url: &str) -> Result<Vec<Entry>> {
    let url = format!("{}/nodes", normalize_index(index_url));
    let resp = reqwest::get(url).await?;
    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(anyhow!("list failed: {}", body));
    }
    let body: ListResponse = resp.json().await?;
    Ok(body.nodes)
}

async fn fetch_peer_ids(index_url: &str, self_id: EndpointId) -> Vec<EndpointId> {
    let entries = match fetch_nodes(index_url).await {
        Ok(nodes) => nodes,
        Err(err) => {
            eprintln!("gossip list fetch failed: {}", err);
            return Vec::new();
        }
    };

    let mut peer_ids = Vec::new();
    for entry in entries {
        match parse_ticket(&entry.ticket) {
            Ok(ticket) => {
                let id = ticket_endpoint_id(&ticket);
                if id != self_id {
                    peer_ids.push(id);
                }
            }
            Err(err) => {
                eprintln!("gossip skip invalid ticket for node={} err={}", entry.node, err);
            }
        }
    }
    peer_ids
}

fn snapshot_nodes(state: &IndexState) -> Result<Vec<Entry>, String> {
    let map = state
        .entries
        .lock()
        .map_err(|_| "index lock poisoned".to_string())?;
    let mut nodes: Vec<Entry> = map.values().cloned().collect();
    nodes.sort_by(|a, b| a.node.cmp(&b.node));
    Ok(nodes)
}

fn publish_nodes(state: &IndexState) {
    if let Ok(nodes) = snapshot_nodes(state) {
        if let Ok(payload) = serde_json::to_string(&ListResponse { nodes }) {
            let _ = state.updates_tx.send(payload);
        }
    }
}

async fn run_node() -> Result<()> {
    let endpoint = Endpoint::bind().await?;
    endpoint.online().await;
    let addr = endpoint.addr();
    let node_name = default_node_name(&addr.id.to_string());
    let index_url = default_index_url();

    let ping = Ping::new();
    let gossip = Gossip::builder().spawn(endpoint.clone());
    let ticket = EndpointTicket::new(addr.clone());
    eprintln!("node id: {}", addr.id);
    for ip in addr.ip_addrs() {
        eprintln!("node ip: {ip}");
    }
    for relay in addr.relay_urls() {
        eprintln!("node relay: {relay}");
    }
    println!("{ticket}");

    if auto_register_enabled() {
        spawn_auto_register(node_name.clone(), ticket.to_string(), index_url.clone());
    }

    let _router = IrohRouter::builder(endpoint)
        .accept(iroh_ping::ALPN, ping)
        .accept(iroh_gossip::ALPN, gossip.clone())
        .spawn();

    let initial_peers = fetch_peer_ids(&index_url, addr.id).await;
    let (gossip_sender, mut gossip_receiver) = gossip.subscribe(GOSSIP_TOPIC, initial_peers).await?.split();

    tokio::spawn(async move {
        while let Some(event) = gossip_receiver.next().await {
            match event {
                Ok(GossipEvent::Received(msg)) => {
                    let text = String::from_utf8_lossy(&msg.content);
                    if let Ok(heartbeat) = serde_json::from_slice::<GossipHeartbeat>(&msg.content) {
                        eprintln!(
                            "gossip heartbeat from={} endpoint_id={} unix={}",
                            heartbeat.node, heartbeat.endpoint_id, heartbeat.unix
                        );
                    } else {
                        eprintln!("gossip received: {}", text);
                    }
                }
                Ok(_) => {}
                Err(err) => {
                    eprintln!("gossip receive error: {}", err);
                    break;
                }
            }
        }
    });

    let heartbeat_node = node_name.clone();
    let heartbeat_index = index_url.clone();
    let heartbeat_id = addr.id.to_string();
    tokio::spawn(async move {
        loop {
            let peer_ids = fetch_peer_ids(&heartbeat_index, addr.id).await;
            if !peer_ids.is_empty() {
                if let Err(err) = gossip_sender.join_peers(peer_ids).await {
                    eprintln!("gossip join_peers error: {}", err);
                }
            }

            let heartbeat = GossipHeartbeat {
                kind: "heartbeat".to_string(),
                node: heartbeat_node.clone(),
                endpoint_id: heartbeat_id.clone(),
                unix: now_unix(),
            };
            match serde_json::to_vec(&heartbeat) {
                Ok(payload) => {
                    if let Err(err) = gossip_sender.broadcast(payload.into()).await {
                        eprintln!("gossip broadcast error: {}", err);
                    }
                }
                Err(err) => eprintln!("gossip heartbeat encode error: {}", err),
            }

            sleep(Duration::from_secs(60)).await;
        }
    });

    tokio::signal::ctrl_c().await?;
    Ok(())
}

async fn run_ping_to_ticket(ticket: EndpointTicket) -> Result<()> {
    let send_ep = Endpoint::bind().await?;
    send_ep.online().await;
    let local_addr = send_ep.addr();
    eprintln!("node id: {}", local_addr.id);
    for ip in local_addr.ip_addrs() {
        eprintln!("node ip: {ip}");
    }
    for relay in local_addr.relay_urls() {
        eprintln!("node relay: {relay}");
    }

    let relay_only = env::var("MESH_V3_RELAY_ONLY").is_ok();
    let mut target_addr = ticket.endpoint_addr().clone();
    if relay_only {
        target_addr.addrs.retain(|addr| addr.is_relay());
        if target_addr.addrs.is_empty() {
            return Err(anyhow!("relay-only mode enabled, but ticket has no relay address"));
        }
        eprintln!("node mode: relay-only");
    }

    let send_pinger = Ping::new();
    let rtt = send_pinger.ping(&send_ep, target_addr).await?;
    println!("ping took: {:?} to complete", rtt);
    Ok(())
}

async fn http_index() -> Html<&'static str> {
    Html(INDEX_HTML)
}

async fn http_register(
    State(state): State<IndexState>,
    Json(req): Json<RegisterRequest>,
) -> Result<Json<RegisterResponse>, String> {
    parse_ticket(&req.ticket).map_err(|e| format!("invalid ticket: {e}"))?;
    let entry = Entry {
        node: req.node.clone(),
        ticket: req.ticket,
        updated_at_unix: now_unix(),
    };
    let mut map = state
        .entries
        .lock()
        .map_err(|_| "index lock poisoned".to_string())?;
    map.insert(req.node, entry.clone());
    drop(map);
    publish_nodes(&state);
    Ok(Json(RegisterResponse { ok: true, entry }))
}

async fn http_get_ticket(
    Path(node): Path<String>,
    State(state): State<IndexState>,
) -> Result<Json<Entry>, String> {
    let map = state
        .entries
        .lock()
        .map_err(|_| "index lock poisoned".to_string())?;
    let entry = map
        .get(&node)
        .ok_or_else(|| format!("node '{node}' not found"))?;
    Ok(Json(entry.clone()))
}

async fn http_list(State(state): State<IndexState>) -> Result<Json<ListResponse>, String> {
    let nodes = snapshot_nodes(&state)?;
    Ok(Json(ListResponse { nodes }))
}

async fn http_put_nodes(
    State(state): State<IndexState>,
    Json(req): Json<PutNodesRequest>,
) -> Result<Json<ListResponse>, String> {
    for n in &req.nodes {
        parse_ticket(&n.ticket).map_err(|e| format!("invalid ticket for '{}': {e}", n.node))?;
    }

    let mut map = state
        .entries
        .lock()
        .map_err(|_| "index lock poisoned".to_string())?;
    map.clear();
    for n in req.nodes {
        map.insert(
            n.node.clone(),
            Entry {
                node: n.node,
                ticket: n.ticket,
                updated_at_unix: now_unix(),
            },
        );
    }
    drop(map);
    publish_nodes(&state);
    let nodes = snapshot_nodes(&state)?;
    Ok(Json(ListResponse { nodes }))
}

async fn ws_handler(ws: WebSocketUpgrade, State(state): State<IndexState>) -> impl IntoResponse {
    ws.on_upgrade(move |socket| ws_connected(socket, state))
}

async fn ws_connected(mut socket: WebSocket, state: IndexState) {
    if let Ok(nodes) = snapshot_nodes(&state) {
        if let Ok(payload) = serde_json::to_string(&ListResponse { nodes }) {
            let _ = socket.send(WsMessage::Text(payload.into())).await;
        }
    }

    let mut rx = state.updates_tx.subscribe();
    loop {
        tokio::select! {
            update = rx.recv() => {
                match update {
                    Ok(payload) => {
                        if socket.send(WsMessage::Text(payload.into())).await.is_err() {
                            break;
                        }
                    }
                    Err(broadcast::error::RecvError::Lagged(_)) => {}
                    Err(broadcast::error::RecvError::Closed) => break,
                }
            }
            msg = socket.recv() => {
                match msg {
                    Some(Ok(WsMessage::Close(_))) | None => break,
                    Some(Ok(_)) => {}
                    Some(Err(_)) => break,
                }
            }
        }
    }
}

async fn run_index(bind: &str) -> Result<()> {
    let (updates_tx, _) = broadcast::channel(128);
    let state = IndexState {
        entries: Arc::new(Mutex::new(HashMap::new())),
        updates_tx,
    };

    let app = Router::new()
        .route("/", get(http_index))
        .route("/health", get(|| async { "ok" }))
        .route("/register", post(http_register))
        .route("/ticket/{node}", get(http_get_ticket))
        .route("/nodes", get(http_list).put(http_put_nodes))
        .route("/ws", get(ws_handler))
        .with_state(state);

    let addr: SocketAddr = bind
        .parse()
        .map_err(|e| anyhow!("invalid bind addr '{bind}': {e}"))?;
    println!("index listening on http://{addr}");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

fn normalize_index(index_url: &str) -> String {
    index_url.trim_end_matches('/').to_string()
}

async fn run_register(index_url: &str, node: &str, ticket: &str) -> Result<()> {
    parse_ticket(ticket).map_err(|e| anyhow!("invalid ticket: {e}"))?;
    let url = format!("{}/register", normalize_index(index_url));
    let client = reqwest::Client::new();
    let req = RegisterRequest {
        node: node.to_string(),
        ticket: ticket.to_string(),
    };
    let resp = client.post(url).json(&req).send().await?;
    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(anyhow!("register failed: {}", body));
    }
    let body: RegisterResponse = resp.json().await?;
    println!(
        "registered node={} updated_at_unix={}",
        body.entry.node, body.entry.updated_at_unix
    );
    Ok(())
}

async fn run_list(index_url: &str) -> Result<()> {
    for entry in fetch_nodes(index_url).await? {
        println!(
            "{} updated={} ticket={}",
            entry.node, entry.updated_at_unix, entry.ticket
        );
    }
    Ok(())
}

async fn run_connect(index_url: &str, node: &str) -> Result<()> {
    let url = format!("{}/ticket/{}", normalize_index(index_url), node);
    let resp = reqwest::get(url).await?;
    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(anyhow!("lookup failed: {}", body));
    }
    let entry: Entry = resp.json().await?;
    let ticket = parse_ticket(&entry.ticket).map_err(|e| anyhow!("invalid ticket: {e}"))?;
    run_ping_to_ticket(ticket).await
}

fn usage() {
    eprintln!("usage:");
    eprintln!("  mesh-v3                # same as 'mesh-v3 node'");
    eprintln!("  mesh-v3 node");
    eprintln!("  mesh-v3 index [bind_addr]");
    eprintln!("  mesh-v3 register <index_url> <node> <ticket>");
    eprintln!("  mesh-v3 list <index_url>");
    eprintln!("  mesh-v3 connect <index_url> <node>");
}

#[tokio::main]
async fn main() -> Result<()> {
    let mut args = env::args().skip(1);
    let role = args.next().unwrap_or_else(|| "node".to_string());

    match role.as_str() {
        "node" => run_node().await,
        "index" => {
            let bind = args.next().unwrap_or_else(|| "0.0.0.0:8787".to_string());
            run_index(&bind).await
        }
        "register" => {
            let index_url = args
                .next()
                .ok_or_else(|| anyhow!("expected index_url as second argument"))?;
            let node = args
                .next()
                .ok_or_else(|| anyhow!("expected node as third argument"))?;
            let ticket = args
                .next()
                .ok_or_else(|| anyhow!("expected ticket as fourth argument"))?;
            run_register(&index_url, &node, &ticket).await
        }
        "list" => {
            let index_url = args
                .next()
                .ok_or_else(|| anyhow!("expected index_url as second argument"))?;
            run_list(&index_url).await
        }
        "connect" => {
            let index_url = args
                .next()
                .ok_or_else(|| anyhow!("expected index_url as second argument"))?;
            let node = args
                .next()
                .ok_or_else(|| anyhow!("expected node as third argument"))?;
            run_connect(&index_url, &node).await
        }
        _ => {
            usage();
            Err(anyhow!("unknown command '{role}'"))
        }
    }
}
