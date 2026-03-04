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
use iroh::{
    Endpoint, EndpointId,
    address_lookup::{DhtAddressLookup, DnsAddressLookup, PkarrPublisher},
    endpoint::RelayMode,
    protocol::Router as IrohRouter,
};
use iroh_gossip::{api::Event as GossipEvent, net::Gossip, proto::TopicId};
use iroh_ping::Ping;
use iroh_tickets::{endpoint::EndpointTicket, Ticket};
use n0_future::StreamExt;
use serde::{Deserialize, Serialize};
use std::{
    collections::HashMap,
    collections::HashSet,
    env,
    fs,
    net::SocketAddr,
    path::Path as FsPath,
    str::FromStr,
    sync::{Arc, Mutex, OnceLock},
    time::{SystemTime, UNIX_EPOCH},
};
use tokio::sync::broadcast;
use tokio::time::{sleep, Duration};
use time::{format_description::well_known::Rfc3339, OffsetDateTime};

#[derive(Clone)]
struct IndexState {
    entries: Arc<Mutex<HashMap<String, Entry>>>,
    updates_tx: broadcast::Sender<String>,
}

#[derive(Clone, Serialize, Deserialize)]
struct Entry {
    node: String,
    #[serde(default)]
    node_id: String,
    ticket: String,
    updated_at_unix: u64,
}

#[derive(Serialize, Deserialize)]
struct RegisterRequest {
    node: String,
    node_id: Option<String>,
    ticket: String,
}

#[derive(Serialize, Deserialize)]
struct RegisterResponse {
    ok: bool,
    entry: Entry,
    #[serde(default)]
    nodes: Vec<Entry>,
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
    node_id: Option<String>,
    ticket: String,
}

#[derive(Serialize, Deserialize)]
struct GossipHeartbeat {
    kind: String,
    node: String,
    endpoint_id: String,
    unix: u64,
    #[serde(default)]
    unix_ms: u64,
    #[serde(default)]
    known_peers: usize,
    peers: Vec<PeerHint>,
}

#[derive(Clone, Serialize, Deserialize)]
struct PeerHint {
    node: String,
    node_id: String,
    ticket: String,
    updated_at_unix: u64,
}

const GOSSIP_TOPIC: TopicId = TopicId::from_bytes(*b"mesh-v3-index-dialtone-heartbeat");
static PREFER_DHT: OnceLock<bool> = OnceLock::new();
const DEFAULT_INDEX_URL: &str = "https://index.dialtone.earth";
const DEFAULT_GOSSIP_INTERVAL_SECS: u64 = 60;
const DEFAULT_REGISTER_INTERVAL_SECS: u64 = 30;

#[derive(Clone)]
struct NodeConfig {
    node_name: Option<String>,
    index_url: String,
    register_interval_secs: u64,
    gossip_interval_secs: u64,
    peer_cache_path: String,
    auto_register: bool,
    dht_enabled: bool,
    dns_enabled: bool,
    relay_only: bool,
}

const INDEX_HTML: &str = include_str!("index.html");

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

fn now_unix_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}

fn iso_now() -> String {
    OffsetDateTime::now_utc()
        .format(&Rfc3339)
        .unwrap_or_else(|_| "1970-01-01T00:00:00Z".to_string())
}

fn parse_ticket(s: &str) -> Result<EndpointTicket> {
    <EndpointTicket as Ticket>::deserialize(s).map_err(|e| anyhow!("failed to parse ticket: {e}"))
}

fn default_node_name(fallback_id: &str) -> String {
    if let Ok(v) = env::var("HOSTNAME") {
        let t = v.trim();
        if !t.is_empty() {
            return t.to_string();
        }
    }
    fallback_id.to_string()
}

fn prefer_dht_enabled() -> bool {
    *PREFER_DHT.get().unwrap_or(&false)
}

async fn build_endpoint(dht_enabled: bool, dns_enabled: bool) -> Result<Endpoint> {
    let mut builder = Endpoint::empty_builder(RelayMode::Default);
    if dns_enabled {
        builder = builder
            .address_lookup(PkarrPublisher::n0_dns())
            .address_lookup(DnsAddressLookup::n0_dns());
    }
    if dht_enabled {
        builder = builder.address_lookup(DhtAddressLookup::builder().n0_dns_pkarr_relay());
    }
    Ok(builder.bind().await?)
}

fn endpoint_id_from_entry(entry: &Entry) -> Option<EndpointId> {
    EndpointId::from_str(entry.node_id.trim()).ok().or_else(|| {
        parse_ticket(&entry.ticket)
            .ok()
            .map(|ticket| ticket_endpoint_id(&ticket))
    })
}

fn normalize_entry(mut entry: Entry) -> Option<Entry> {
    if entry.node_id.trim().is_empty() {
        entry.node_id = endpoint_id_from_entry(&entry)?.to_string();
    }
    Some(entry)
}

fn retry_delay_secs(attempt: u32) -> u64 {
    match attempt {
        0 => 5,
        1 => 10,
        2 => 20,
        3 => 40,
        _ => 60,
    }
}

fn default_peer_cache_path() -> String {
    let home = env::var("HOME").unwrap_or_else(|_| ".".to_string());
    format!("{home}/dialtone/tmp/mesh-v3-peer-cache.json")
}

fn load_cache_file(path: &str) -> Vec<Entry> {
    let Ok(raw) = fs::read_to_string(path) else {
        return Vec::new();
    };
    let Ok(entries) = serde_json::from_str::<Vec<Entry>>(&raw) else {
        return Vec::new();
    };
    entries
        .into_iter()
        .filter_map(normalize_entry)
        .collect::<Vec<_>>()
}

fn write_cache_file(path: &str, entries: &[Entry]) {
    if let Some(parent) = FsPath::new(path).parent() {
        let _ = fs::create_dir_all(parent);
    }
    if let Ok(raw) = serde_json::to_string(entries) {
        let _ = fs::write(path, raw);
    }
}

fn merge_entries(cache: &Arc<Mutex<HashMap<String, Entry>>>, entries: &[Entry]) {
    if let Ok(mut map) = cache.lock() {
        for entry in entries {
            let Some(entry) = normalize_entry(entry.clone()) else {
                continue;
            };
            let same_node_keys: Vec<String> = map
                .iter()
                .filter(|(_, v)| v.node == entry.node)
                .map(|(k, _)| k.clone())
                .collect();
            let has_newer_same_node = same_node_keys
                .iter()
                .filter_map(|k| map.get(k))
                .any(|v| v.updated_at_unix > entry.updated_at_unix);
            if has_newer_same_node {
                continue;
            }
            for k in same_node_keys {
                map.remove(&k);
            }
            map.insert(entry.node_id.clone(), entry.clone());
        }
    }
}

fn cache_entries(cache: &Arc<Mutex<HashMap<String, Entry>>>) -> Vec<Entry> {
    if let Ok(map) = cache.lock() {
        return map.values().cloned().collect();
    }
    Vec::new()
}

fn known_peer_count(cache: &Arc<Mutex<HashMap<String, Entry>>>, self_id: &str) -> usize {
    cache_entries(cache)
        .into_iter()
        .filter(|e| e.node_id != self_id)
        .count()
}

fn log_peer_count(host: &str, cache: &Arc<Mutex<HashMap<String, Entry>>>, self_id: &str) {
    eprintln!("{} {} {}", iso_now(), host, known_peer_count(cache, self_id));
}

fn log_line(host: &str, peer_count: usize) {
    eprintln!("{} {} {}", iso_now(), host, peer_count);
}

fn log_event(host: &str, peer_count: usize, event: &str) {
    eprintln!("{} {} {} {}", iso_now(), host, peer_count, event);
}

fn log_dht_lookup(host: &str, peer_count: usize, targets: usize) {
    eprintln!(
        "{} {} {} dht_lookup targets={}",
        iso_now(),
        host,
        peer_count,
        targets
    );
}

fn log_lookup_stack(host: &str, peer_count: usize, dht_enabled: bool, dns_enabled: bool) {
    let dht = if dht_enabled { "on" } else { "off" };
    let dns = if dns_enabled { "on" } else { "off" };
    let prefer_dht = if prefer_dht_enabled() { "on" } else { "off" };
    eprintln!(
        "{} {} {} lookup_stack dht={} dns={} prefer_dht={} relay=default",
        iso_now(),
        host,
        peer_count,
        dht,
        dns,
        prefer_dht
    );
}

fn log_event_latency(host: &str, peer_count: usize, event: &str, latency_ms: u64) {
    eprintln!(
        "{} {} {} {} latency_ms={}",
        iso_now(),
        host,
        peer_count,
        event,
        latency_ms
    );
}

fn entries_to_peer_ids(entries: &[Entry], self_id: EndpointId) -> Vec<EndpointId> {
    let mut out = Vec::new();
    for entry in entries.iter().cloned().filter_map(normalize_entry) {
        if let Some(id) = endpoint_id_from_entry(&entry) {
            if id != self_id {
                out.push(id);
            }
        }
    }
    out.sort();
    out.dedup();
    out
}

async fn fetch_nodes_with_retry(index_url: &str) -> Option<Vec<Entry>> {
    for attempt in 0..3 {
        match fetch_nodes(index_url).await {
            Ok(nodes) => return Some(nodes),
            Err(_) => {
                if attempt < 2 {
                    sleep(Duration::from_secs(retry_delay_secs(attempt))).await;
                }
            }
        }
    }
    None
}

fn spawn_auto_register(
    node: String,
    node_id: String,
    endpoint: Endpoint,
    index_url: String,
    register_interval_secs: u64,
    peer_cache: Arc<Mutex<HashMap<String, Entry>>>,
) {
    tokio::spawn(async move {
        let client = reqwest::Client::new();
        let url = format!("{}/register", normalize_index(&index_url));
        let mut failure_attempt = 0u32;
        loop {
            let ticket = EndpointTicket::new(endpoint.addr()).to_string();
            let req = RegisterRequest {
                node: node.clone(),
                node_id: Some(node_id.clone()),
                ticket,
            };
            let res = client.post(&url).json(&req).send().await;
            match res {
                Ok(resp) if resp.status().is_success() => {
                    match resp.json::<RegisterResponse>().await {
                        Ok(body) => {
                            merge_entries(&peer_cache, &body.nodes);
                            log_event(
                                &node,
                                known_peer_count(&peer_cache, &node_id),
                                "register",
                            );
                        }
                        Err(err) => {
                            let _ = err;
                            log_peer_count(&node, &peer_cache, &node_id);
                        }
                    }
                    failure_attempt = 0;
                }
                Ok(resp) => {
                    let _ = resp;
                    log_peer_count(&node, &peer_cache, &node_id);
                    failure_attempt = failure_attempt.saturating_add(1);
                }
                Err(err) => {
                    let _ = err;
                    log_peer_count(&node, &peer_cache, &node_id);
                    failure_attempt = failure_attempt.saturating_add(1);
                }
            }
            let delay = if failure_attempt == 0 {
                register_interval_secs
            } else {
                retry_delay_secs(failure_attempt.saturating_sub(1))
            };
            sleep(Duration::from_secs(delay)).await;
        }
    });
}

fn ticket_endpoint_id(ticket: &EndpointTicket) -> EndpointId {
    ticket.endpoint_addr().id
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
    match fetch_nodes_with_retry(index_url).await {
        Some(entries) => entries_to_peer_ids(&entries, self_id),
        None => Vec::new(),
    }
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

async fn run_node(config: NodeConfig) -> Result<()> {
    let endpoint = build_endpoint(config.dht_enabled, config.dns_enabled).await?;
    endpoint.online().await;
    let addr = endpoint.addr();
    let node_name = config
        .node_name
        .clone()
        .unwrap_or_else(|| default_node_name(&addr.id.to_string()));
    let index_url = config.index_url.clone();

    let ping = Ping::new();
    let gossip = Gossip::builder().spawn(endpoint.clone());
    let ticket = EndpointTicket::new(addr.clone());
    let peer_cache: Arc<Mutex<HashMap<String, Entry>>> = Arc::new(Mutex::new(HashMap::new()));
    let dht_pending: Arc<Mutex<HashSet<String>>> = Arc::new(Mutex::new(HashSet::new()));
    let peer_cache_path = config.peer_cache_path.clone();
    merge_entries(&peer_cache, &load_cache_file(&peer_cache_path));
    merge_entries(
        &peer_cache,
        &[Entry {
            node: node_name.clone(),
            node_id: addr.id.to_string(),
            ticket: ticket.to_string(),
            updated_at_unix: now_unix(),
        }],
    );
    log_peer_count(&node_name, &peer_cache, &addr.id.to_string());
    log_lookup_stack(
        &node_name,
        known_peer_count(&peer_cache, &addr.id.to_string()),
        config.dht_enabled,
        config.dns_enabled,
    );
    println!("{ticket}");

    if config.auto_register {
        spawn_auto_register(
            node_name.clone(),
            addr.id.to_string(),
            endpoint.clone(),
            index_url.clone(),
            config.register_interval_secs,
            peer_cache.clone(),
        );
    }

    let _router = IrohRouter::builder(endpoint)
        .accept(iroh_ping::ALPN, ping)
        .accept(iroh_gossip::ALPN, gossip.clone())
        .spawn();

    if let Some(nodes) = fetch_nodes_with_retry(&index_url).await {
        merge_entries(&peer_cache, &nodes);
    }
    let initial_peers = entries_to_peer_ids(&cache_entries(&peer_cache), addr.id);
    let (gossip_sender, mut gossip_receiver) = gossip.subscribe(GOSSIP_TOPIC, initial_peers).await?.split();

    let receiver_cache = peer_cache.clone();
    let receiver_pending = dht_pending.clone();
    let receiver_host = node_name.clone();
    let receiver_self_id = addr.id.to_string();
    let receiver_dht_enabled = config.dht_enabled;
    tokio::spawn(async move {
        while let Some(event) = gossip_receiver.next().await {
            match event {
                Ok(GossipEvent::Received(msg)) => {
                    if let Ok(heartbeat) = serde_json::from_slice::<GossipHeartbeat>(&msg.content) {
                        let before_nodes: HashSet<String> = cache_entries(&receiver_cache)
                            .into_iter()
                            .map(|e| e.node)
                            .collect();
                        let endpoint_id = heartbeat.endpoint_id.clone();
                        if !heartbeat.peers.is_empty() {
                            let hinted: Vec<Entry> = heartbeat
                                .peers
                                .iter()
                                .map(|p| Entry {
                                    node: p.node.clone(),
                                    node_id: p.node_id.clone(),
                                    ticket: p.ticket.clone(),
                                    updated_at_unix: p.updated_at_unix,
                                })
                                .collect();
                            merge_entries(&receiver_cache, &hinted);
                            let after = cache_entries(&receiver_cache);
                            let total = after
                                .iter()
                                .filter(|e| e.node_id != receiver_self_id)
                                .count();
                            for e in after {
                                if e.node_id != receiver_self_id && !before_nodes.contains(&e.node) {
                                    log_event(&receiver_host, total, &format!("join:{}", e.node));
                                }
                            }
                        }
                        if !before_nodes.contains(&heartbeat.node) && receiver_dht_enabled {
                            let mut resolved_via_dht = false;
                            if let Ok(mut pending) = receiver_pending.lock() {
                                resolved_via_dht = pending.remove(&endpoint_id);
                            }
                            if resolved_via_dht {
                                log_event(
                                    &receiver_host,
                                    known_peer_count(&receiver_cache, &receiver_self_id),
                                    &format!("dht_found:{}", heartbeat.node),
                                );
                            }
                        }
                        let sent_ms = if heartbeat.unix_ms > 0 {
                            heartbeat.unix_ms
                        } else {
                            heartbeat.unix.saturating_mul(1000)
                        };
                        let latency_ms = now_unix_ms().saturating_sub(sent_ms);
                        log_event_latency(
                            &heartbeat.node,
                            heartbeat.known_peers,
                            "heartbeat",
                            latency_ms,
                        );
                        log_peer_count(&receiver_host, &receiver_cache, &receiver_self_id);
                    } else if let Ok(v) = serde_json::from_slice::<serde_json::Value>(&msg.content) {
                        if v.get("kind").and_then(|k| k.as_str()) == Some("heartbeat") {
                            if let Some(node) = v.get("node").and_then(|n| n.as_str()) {
                                let sent_ms = v
                                    .get("unix_ms")
                                    .and_then(|x| x.as_u64())
                                    .or_else(|| {
                                        v.get("unix")
                                            .and_then(|x| x.as_u64())
                                            .map(|s| s.saturating_mul(1000))
                                    })
                                    .unwrap_or(0);
                                let latency_ms = now_unix_ms().saturating_sub(sent_ms);
                                log_event_latency(node, 0, "heartbeat", latency_ms);
                            }
                            log_peer_count(&receiver_host, &receiver_cache, &receiver_self_id);
                        }
                    }
                }
                Ok(_) => {}
                Err(_) => {
                    break;
                }
            }
        }
    });

    let heartbeat_node = node_name.clone();
    let heartbeat_index = index_url.clone();
    let heartbeat_id = addr.id.to_string();
    let heartbeat_cache = peer_cache.clone();
    let heartbeat_pending = dht_pending.clone();
    let heartbeat_dht_enabled = config.dht_enabled;
    let heartbeat_interval_secs = config.gossip_interval_secs;
    tokio::spawn(async move {
        loop {
            if let Some(nodes) = fetch_nodes_with_retry(&heartbeat_index).await {
                merge_entries(&heartbeat_cache, &nodes);
            }

            let entries = cache_entries(&heartbeat_cache);
            let peer_ids = entries_to_peer_ids(&entries, addr.id);
            if !peer_ids.is_empty() {
                if heartbeat_dht_enabled {
                    if let Ok(mut pending) = heartbeat_pending.lock() {
                        for id in &peer_ids {
                            pending.insert(id.to_string());
                        }
                    }
                    log_dht_lookup(
                        &heartbeat_node,
                        known_peer_count(&heartbeat_cache, &heartbeat_id),
                        peer_ids.len(),
                    );
                }
                let _ = gossip_sender.join_peers(peer_ids).await;
            }

            let peer_hints: Vec<PeerHint> = entries
                .into_iter()
                .filter(|e| e.node_id != heartbeat_id)
                .take(32)
                .map(|e| PeerHint {
                    node: e.node,
                    node_id: e.node_id,
                    ticket: e.ticket,
                    updated_at_unix: e.updated_at_unix,
                })
                .collect();
            let heartbeat = GossipHeartbeat {
                kind: "heartbeat".to_string(),
                node: heartbeat_node.clone(),
                endpoint_id: heartbeat_id.clone(),
                unix: now_unix(),
                unix_ms: now_unix_ms(),
                known_peers: known_peer_count(&heartbeat_cache, &heartbeat_id),
                peers: peer_hints,
            };
            match serde_json::to_vec(&heartbeat) {
                Ok(payload) => {
                    let _ = gossip_sender.broadcast(payload.into()).await;
                }
                Err(_) => {}
            }
            log_peer_count(&heartbeat_node, &heartbeat_cache, &heartbeat_id);

            sleep(Duration::from_secs(heartbeat_interval_secs)).await;
        }
    });

    let persist_cache = peer_cache.clone();
    let persist_path = peer_cache_path.clone();
    tokio::spawn(async move {
        loop {
            write_cache_file(&persist_path, &cache_entries(&persist_cache));
            sleep(Duration::from_secs(30)).await;
        }
    });

    tokio::signal::ctrl_c().await?;
    Ok(())
}

async fn run_ping_to_ticket(ticket: EndpointTicket, config: &NodeConfig) -> Result<()> {
    let send_ep = build_endpoint(config.dht_enabled, config.dns_enabled).await?;
    send_ep.online().await;
    let local_addr = send_ep.addr();
    eprintln!("{} {} {}", iso_now(), local_addr.id, 0);

    let relay_only = config.relay_only;
    let mut target_addr = ticket.endpoint_addr().clone();
    if relay_only {
        target_addr.addrs.retain(|addr| addr.is_relay());
        if target_addr.addrs.is_empty() {
            return Err(anyhow!("relay-only mode enabled, but ticket has no relay address"));
        }
        eprintln!("{} {} {}", iso_now(), local_addr.id, 0);
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
    let node_id = req
        .node_id
        .as_ref()
        .and_then(|id| EndpointId::from_str(id.trim()).ok())
        .or_else(|| parse_ticket(&req.ticket).ok().map(|t| ticket_endpoint_id(&t)))
        .map(|id| id.to_string())
        .ok_or_else(|| "unable to resolve node_id from request".to_string())?;
    let entry = Entry {
        node: req.node.clone(),
        node_id: node_id.clone(),
        ticket: req.ticket,
        updated_at_unix: now_unix(),
    };
    let mut map = state
        .entries
        .lock()
        .map_err(|_| "index lock poisoned".to_string())?;
    map.retain(|_, v| v.node != req.node);
    map.insert(node_id, entry.clone());
    drop(map);
    publish_nodes(&state);
    let nodes = snapshot_nodes(&state)?;
    Ok(Json(RegisterResponse {
        ok: true,
        entry,
        nodes,
    }))
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
        let node_id = n
            .node_id
            .as_ref()
            .and_then(|id| EndpointId::from_str(id.trim()).ok())
            .or_else(|| parse_ticket(&n.ticket).ok().map(|t| ticket_endpoint_id(&t)))
            .map(|id| id.to_string())
            .ok_or_else(|| format!("invalid node_id for '{}'", n.node))?;
        map.insert(
            node_id.clone(),
            Entry {
                node: n.node,
                node_id,
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

async fn run_hub(bind: &str, config: NodeConfig) -> Result<()> {
    let index_task = tokio::spawn({
        let bind = bind.to_string();
        async move { run_index(&bind).await }
    });
    let node_task = tokio::spawn(async move { run_node(config).await });

    tokio::select! {
        r = index_task => {
            r.map_err(|e| anyhow!("index task join error: {e}"))??;
            Ok(())
        }
        r = node_task => {
            r.map_err(|e| anyhow!("node task join error: {e}"))??;
            Ok(())
        }
    }
}

fn normalize_index(index_url: &str) -> String {
    index_url.trim_end_matches('/').to_string()
}

async fn run_register(index_url: &str, node: &str, ticket: &str) -> Result<()> {
    let parsed = parse_ticket(ticket).map_err(|e| anyhow!("invalid ticket: {e}"))?;
    let node_id = ticket_endpoint_id(&parsed).to_string();
    let url = format!("{}/register", normalize_index(index_url));
    let client = reqwest::Client::new();
    let req = RegisterRequest {
        node: node.to_string(),
        node_id: Some(node_id),
        ticket: ticket.to_string(),
    };
    let resp = client.post(url).json(&req).send().await?;
    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(anyhow!("register failed: {}", body));
    }
    let body: RegisterResponse = resp.json().await?;
    println!(
        "registered node={} updated_at_unix={} peers={}",
        body.entry.node,
        body.entry.updated_at_unix,
        body.nodes.len()
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

async fn run_connect(index_url: &str, node: &str, config: &NodeConfig) -> Result<()> {
    let url = format!("{}/ticket/{}", normalize_index(index_url), node);
    let resp = reqwest::get(url).await?;
    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(anyhow!("lookup failed: {}", body));
    }
    let entry: Entry = resp.json().await?;
    let ticket = parse_ticket(&entry.ticket).map_err(|e| anyhow!("invalid ticket: {e}"))?;
    run_ping_to_ticket(ticket, config).await
}

fn usage() {
    eprintln!("usage:");
    eprintln!("  mesh-v3 [flags]                # same as 'mesh-v3 node'");
    eprintln!("  mesh-v3 [flags] node");
    eprintln!("  mesh-v3 [flags] index [bind_addr]");
    eprintln!("  mesh-v3 [flags] hub [bind_addr]  # run index + node together");
    eprintln!("  mesh-v3 [flags] register <index_url> <node> <ticket>");
    eprintln!("  mesh-v3 [flags] list <index_url>");
    eprintln!("  mesh-v3 [flags] connect <index_url> <node>");
    eprintln!("flags:");
    eprintln!("  --node <name>");
    eprintln!("  --index-url <url>");
    eprintln!("  --gossip-interval <secs>");
    eprintln!("  --register-interval <secs>");
    eprintln!("  --peer-cache <path>");
    eprintln!("  --no-auto-register");
    eprintln!("  --dht | --no-dht");
    eprintln!("  --dns | --no-dns");
    eprintln!("  --relay-only");
}

#[tokio::main]
async fn main() -> Result<()> {
    let mut role: Option<String> = None;
    let mut rest: Vec<String> = Vec::new();

    let mut node_name: Option<String> = None;
    let mut index_url = DEFAULT_INDEX_URL.to_string();
    let mut gossip_interval_secs = DEFAULT_GOSSIP_INTERVAL_SECS;
    let mut register_interval_secs = DEFAULT_REGISTER_INTERVAL_SECS;
    let mut peer_cache_path = default_peer_cache_path();
    let mut auto_register = true;
    let mut dht_enabled = true;
    let mut dns_enabled = true;
    let mut relay_only = false;
    let mut prefer_dht = false;

    let argv: Vec<String> = env::args().skip(1).collect();
    let mut i = 0usize;
    while i < argv.len() {
        let arg = &argv[i];
        let next = |idx: &mut usize| -> Result<String> {
            *idx += 1;
            argv.get(*idx)
                .cloned()
                .ok_or_else(|| anyhow!("missing value for {}", argv[*idx - 1]))
        };
        match arg.as_str() {
            "node" | "index" | "hub" | "register" | "list" | "connect" if role.is_none() => {
                role = Some(arg.clone());
            }
            "--node" => node_name = Some(next(&mut i)?),
            "--index-url" => index_url = next(&mut i)?,
            "--gossip-interval" => {
                gossip_interval_secs = next(&mut i)?
                    .parse::<u64>()
                    .map_err(|_| anyhow!("invalid --gossip-interval"))?;
                if gossip_interval_secs < 10 {
                    return Err(anyhow!("--gossip-interval must be >= 10"));
                }
            }
            "--register-interval" => {
                register_interval_secs = next(&mut i)?
                    .parse::<u64>()
                    .map_err(|_| anyhow!("invalid --register-interval"))?;
                if register_interval_secs < 5 {
                    return Err(anyhow!("--register-interval must be >= 5"));
                }
            }
            "--peer-cache" => peer_cache_path = next(&mut i)?,
            "--no-auto-register" => auto_register = false,
            "--dht" => {
                dht_enabled = true;
                prefer_dht = true;
                dns_enabled = false;
            }
            "--no-dht" => dht_enabled = false,
            "--dns" => dns_enabled = true,
            "--no-dns" => dns_enabled = false,
            "--relay-only" => relay_only = true,
            other => rest.push(other.to_string()),
        }
        i += 1;
    }

    let role = role.unwrap_or_else(|| "node".to_string());
    let _ = PREFER_DHT.set(prefer_dht);
    let config = NodeConfig {
        node_name,
        index_url: index_url.clone(),
        register_interval_secs,
        gossip_interval_secs,
        peer_cache_path,
        auto_register,
        dht_enabled,
        dns_enabled,
        relay_only,
    };

    match role.as_str() {
        "node" => run_node(config).await,
        "index" => {
            let bind = rest
                .first()
                .cloned()
                .unwrap_or_else(|| "0.0.0.0:8787".to_string());
            run_index(&bind).await
        }
        "hub" => {
            let bind = rest
                .first()
                .cloned()
                .unwrap_or_else(|| "0.0.0.0:8787".to_string());
            run_hub(&bind, config).await
        }
        "register" => {
            let index_url = rest
                .first()
                .cloned()
                .ok_or_else(|| anyhow!("expected index_url as second argument"))?;
            let node = rest
                .get(1)
                .cloned()
                .ok_or_else(|| anyhow!("expected node as third argument"))?;
            let ticket = rest
                .get(2)
                .cloned()
                .ok_or_else(|| anyhow!("expected ticket as fourth argument"))?;
            run_register(&index_url, &node, &ticket).await
        }
        "list" => {
            let index_url = rest
                .first()
                .cloned()
                .ok_or_else(|| anyhow!("expected index_url as second argument"))?;
            run_list(&index_url).await
        }
        "connect" => {
            let index_url = rest
                .first()
                .cloned()
                .ok_or_else(|| anyhow!("expected index_url as second argument"))?;
            let node = rest
                .get(1)
                .cloned()
                .ok_or_else(|| anyhow!("expected node as third argument"))?;
            run_connect(&index_url, &node, &config).await
        }
        _ => {
            usage();
            Err(anyhow!("unknown command '{role}'"))
        }
    }
}
