use anyhow::{anyhow, Result};
use iroh::{protocol::Router, Endpoint};
use iroh_ping::Ping;
use iroh_tickets::{endpoint::EndpointTicket, Ticket};
use std::env;

async fn run_receiver() -> Result<()> {
    let endpoint = Endpoint::bind().await?;
    endpoint.online().await;
    let addr = endpoint.addr();

    let ping = Ping::new();
    let ticket = EndpointTicket::new(addr.clone());
    eprintln!("receiver id: {}", addr.id);
    for ip in addr.ip_addrs() {
        eprintln!("receiver ip: {ip}");
    }
    for relay in addr.relay_urls() {
        eprintln!("receiver relay: {relay}");
    }
    println!("{ticket}");

    let _router = Router::builder(endpoint)
        .accept(iroh_ping::ALPN, ping)
        .spawn();

    tokio::signal::ctrl_c().await?;
    Ok(())
}

async fn run_sender(ticket: EndpointTicket) -> Result<()> {
    let send_ep = Endpoint::bind().await?;
    send_ep.online().await;
    let local_addr = send_ep.addr();
    eprintln!("sender id: {}", local_addr.id);
    for ip in local_addr.ip_addrs() {
        eprintln!("sender ip: {ip}");
    }
    for relay in local_addr.relay_urls() {
        eprintln!("sender relay: {relay}");
    }

    let relay_only = env::var("MESH_V3_RELAY_ONLY").is_ok();
    let mut target_addr = ticket.endpoint_addr().clone();
    if relay_only {
        target_addr.addrs.retain(|addr| addr.is_relay());
        if target_addr.addrs.is_empty() {
            return Err(anyhow!("relay-only mode enabled, but ticket has no relay address"));
        }
        eprintln!("sender mode: relay-only");
    }

    let send_pinger = Ping::new();
    let rtt = send_pinger.ping(&send_ep, target_addr).await?;

    println!("ping took: {:?} to complete", rtt);
    Ok(())
}

fn run_inspect(ticket: EndpointTicket) {
    let addr = ticket.endpoint_addr();
    println!("endpoint id: {}", addr.id);
    for ip in addr.ip_addrs() {
        println!("ticket ip: {ip}");
    }
    for relay in addr.relay_urls() {
        println!("ticket relay: {relay}");
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    let mut args = env::args().skip(1);
    let role = args.next().ok_or_else(|| {
        anyhow!("expected 'receiver', 'sender', or 'inspect' as the first argument")
    })?;

    match role.as_str() {
        "receiver" => run_receiver().await,
        "sender" => {
            let ticket_str = args
                .next()
                .ok_or_else(|| anyhow!("expected ticket as the second argument"))?;
            let ticket = EndpointTicket::deserialize(&ticket_str)
                .map_err(|e| anyhow!("failed to parse ticket: {e}"))?;

            run_sender(ticket).await
        }
        "inspect" => {
            let ticket_str = args
                .next()
                .ok_or_else(|| anyhow!("expected ticket as the second argument"))?;
            let ticket = EndpointTicket::deserialize(&ticket_str)
                .map_err(|e| anyhow!("failed to parse ticket: {e}"))?;
            run_inspect(ticket);
            Ok(())
        }
        _ => Err(anyhow!("unknown role '{role}'; use 'receiver', 'sender', or 'inspect'")),
    }
}
