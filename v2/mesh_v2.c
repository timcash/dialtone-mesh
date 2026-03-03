#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#define shell_close closesocket
#else
#include <arpa/inet.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <unistd.h>
#include <dirent.h>
#define shell_close close
#endif

#include <errno.h>
#include <inttypes.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <uv.h>
#include "udx.h"

#define DEFAULT_NODES_DIR "nodes"
#define DEFAULT_RELAY_HOST "127.0.0.1"
#define DEFAULT_RELAY_PORT 8080
#define HEARTBEAT_INTERVAL_MS 5000
#define DISCOVERY_INTERVAL_MS 10000

typedef struct app_s {
  uv_loop_t *loop;
  udx_t udx;
  udx_socket_t socket;
  udx_stream_t stream;
  uv_timer_t heartbeat_timer;
  uv_timer_t discovery_timer;
  bool has_peer;
  uint32_t local_id;
  uint32_t peer_id;
  bool is_relay;
  uint32_t http_port;
  char nodes_dir[1024];
  char node_name[256];
  char relay_host[256];
  uint32_t relay_port;
  uv_tcp_t registration_client;
  uv_connect_t registration_connect;
  
  // Discovery state
  uv_tcp_t *discovery_client;
  uv_connect_t discovery_connect;
  bool discovery_active;
} app_t;

static void alloc_buffer(uv_handle_t *handle, size_t suggested_size, uv_buf_t *buf) {
  buf->base = malloc(suggested_size);
  buf->len = suggested_size;
}

static int write_file(const char *path, const char *data, size_t len) {
  FILE *f = fopen(path, "wb");
  if (!f) return -1;
  fwrite(data, 1, len, f);
  fclose(f);
  return 0;
}

static int send_all(int fd, const char *buf, size_t len) {
  size_t written = 0;
  while (written < len) {
    ssize_t n = send(fd, buf + written, len - written, 0);
    if (n <= 0) return -1;
    written += (size_t) n;
  }
  return 0;
}

static void serve_mesh_json(int cfd, const char *dir_path) {
  DIR *d = opendir(dir_path);
  char *json = malloc(1024 * 64);
  strcpy(json, "[");
  bool first = true;
  if (d) {
    struct dirent *entry;
    while ((entry = readdir(d)) != NULL) {
      if (entry->d_name[0] == '.') continue;
      char path[1024];
      snprintf(path, sizeof(path), "%s/%s", dir_path, entry->d_name);
      FILE *f = fopen(path, "rb");
      if (f) {
        if (!first) strcat(json, ",");
        first = false;
        char buf[1024];
        size_t n = fread(buf, 1, sizeof(buf) - 1, f);
        buf[n] = '\0';
        strcat(json, buf);
        fclose(f);
      }
    }
    closedir(d);
  }
  strcat(json, "]");
  char header[256];
  int h_len = snprintf(header, sizeof(header), 
    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n", 
    strlen(json));
  send_all(cfd, header, h_len);
  send_all(cfd, json, strlen(json));
  free(json);
}

static int run_relay_server(app_t *app) {
  mkdir(app->nodes_dir, 0755);
  int sfd = socket(AF_INET, SOCK_STREAM, 0);
  int opt = 1;
  setsockopt(sfd, SOL_SOCKET, SO_REUSEADDR, (const char *)&opt, sizeof(opt));
  struct sockaddr_in addr;
  memset(&addr, 0, sizeof(addr));
  addr.sin_family = AF_INET;
  addr.sin_port = htons((uint16_t) app->http_port);
  addr.sin_addr.s_addr = INADDR_ANY;
  bind(sfd, (struct sockaddr *) &addr, sizeof(addr));
  listen(sfd, 64);
  printf("mesh v2 relay listening on http://0.0.0.0:%u\n", app->http_port);
  fflush(stdout);
  while (1) {
    int cfd = accept(sfd, NULL, NULL);
    if (cfd < 0) continue;
    char req[8192]; memset(req, 0, sizeof(req));
    ssize_t total_read = recv(cfd, req, sizeof(req) - 1, 0);
    if (total_read <= 0) { shell_close(cfd); continue; }
    if (strncmp(req, "GET /", 5) == 0) serve_mesh_json(cfd, app->nodes_dir);
    else if (strncmp(req, "PUT /nodes/", 11) == 0) {
      char *name_start = req + 11;
      char *name_end = strchr(name_start, ' ');
      if (name_end) {
        char node_name[256];
        size_t name_len = name_end - name_start;
        if (name_len > 255) name_len = 255;
        memcpy(node_name, name_start, name_len);
        node_name[name_len] = '\0';
        char *body = strstr(req, "\r\n\r\n");
        if (body) {
          body += 4;
          char path[1024];
          snprintf(path, sizeof(path), "%s/%s.json", app->nodes_dir, node_name);
          write_file(path, body, strlen(body));
          const char *resp = "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok";
          send_all(cfd, resp, strlen(resp));
          printf("relay: registered node %s\n", node_name);
          fflush(stdout);
        }
      }
    }
    shell_close(cfd);
  }
  return 0;
}

static void on_registration_connect(uv_connect_t *req, int status) {
  app_t *app = req->data;
  if (status < 0) { printf("mesh: registration failed\n"); fflush(stdout); return; }
  char body[512];
  int body_len = snprintf(body, sizeof(body), "{\"name\": \"%s\", \"host\": \"auto\", \"port\": %u}", app->node_name, 18080);
  char header[1024];
  int h_len = snprintf(header, sizeof(header), "PUT /nodes/%s HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", app->node_name, app->relay_host, body_len);
  uv_buf_t bufs[2];
  bufs[0] = uv_buf_init(header, h_len);
  bufs[1] = uv_buf_init(body, body_len);
  uv_write_t *w_req = malloc(sizeof(uv_write_t));
  uv_write(w_req, (uv_stream_t*)&app->registration_client, bufs, 2, NULL);
  printf("mesh: registration sent for %s\n", app->node_name);
  fflush(stdout);
}

static void on_heartbeat_cb(udx_stream_write_t *req, int status, int unordered) {
  (void) status; (void) unordered; free(req);
}

static void on_heartbeat_tick(uv_timer_t *t) {
  app_t *app = t->data;
  if (!app->has_peer) return;
  char msg[256];
  int len = snprintf(msg, sizeof(msg), "heartbeat-from-%s", app->node_name);
  uv_buf_t buf = uv_buf_init(msg, len);
  udx_stream_write_t *req = malloc(udx_stream_write_sizeof(1));
  udx_stream_write(req, &app->stream, &buf, 1, on_heartbeat_cb);
  printf("mesh: sent heartbeat\n");
  fflush(stdout);
}

static void on_discovery_read(uv_stream_t *stream, ssize_t nread, const uv_buf_t *buf) {
  app_t *app = stream->data;
  if (nread > 0) {
    char *json = strstr(buf->base, "\r\n\r\n");
    if (json) {
      json += 4;
      printf("mesh: discovered peers: %s\n", json);
      fflush(stdout);
      // Logic: Connect to any node that isn't us
      if (!app->has_peer) {
        char search_gold[] = "\"name\": \"gold\"";
        char search_wsl[] = "\"name\": \"wsl\"";
        if (strstr(json, search_gold) && strcmp(app->node_name, "gold") != 0) {
          struct sockaddr_in paddr; uv_ip4_addr("192.168.4.55", 18080, &paddr);
          udx_stream_connect(&app->stream, &app->socket, 2, (struct sockaddr *)&paddr);
          app->has_peer = true; printf("mesh: connected to gold\n"); fflush(stdout);
        } else if (strstr(json, search_wsl) && strcmp(app->node_name, "wsl") != 0) {
          struct sockaddr_in paddr; uv_ip4_addr("192.168.4.52", 18080, &paddr);
          udx_stream_connect(&app->stream, &app->socket, 1, (struct sockaddr *)&paddr);
          app->has_peer = true; printf("mesh: connected to wsl\n"); fflush(stdout);
        }
      }
    }
  }
  free(buf->base);
  app->discovery_active = false;
  uv_close((uv_handle_t*)stream, (uv_close_cb)free);
  app->discovery_client = NULL;
}

static void on_discovery_connect(uv_connect_t *req, int status) {
  app_t *app = req->data;
  if (status < 0) { app->discovery_active = false; return; }
  char header[256];
  int h_len = snprintf(header, sizeof(header), "GET /mesh.json HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", app->relay_host);
  uv_buf_t buf = uv_buf_init(header, h_len);
  uv_write_t *wreq = malloc(sizeof(uv_write_t));
  uv_write(wreq, (uv_stream_t*)app->discovery_client, &buf, 1, (uv_write_cb)free);
  uv_read_start((uv_stream_t*)app->discovery_client, alloc_buffer, on_discovery_read);
}

static void on_discovery_tick(uv_timer_t *t) {
  app_t *app = t->data;
  if (app->discovery_active) return;
  app->discovery_active = true;
  app->discovery_client = malloc(sizeof(uv_tcp_t));
  uv_tcp_init(app->loop, app->discovery_client);
  app->discovery_client->data = app;
  struct sockaddr_in raddr; uv_ip4_addr(app->relay_host, app->relay_port, &raddr);
  uv_tcp_connect(&app->discovery_connect, app->discovery_client, (struct sockaddr*)&raddr, on_discovery_connect);
}

static void on_read(udx_stream_t *stream, ssize_t nread, const uv_buf_t *buf) {
  if (nread > 0) { printf("mesh: received data: %.*s\n", (int) nread, buf->base); fflush(stdout); }
}

int main(int argc, char **argv) {
  app_t app; memset(&app, 0, sizeof(app));
  app.http_port = 8080; app.local_id = 1; app.peer_id = 2;
  strncpy(app.nodes_dir, DEFAULT_NODES_DIR, sizeof(app.nodes_dir));
  strncpy(app.node_name, "unknown", sizeof(app.node_name));
  strncpy(app.relay_host, DEFAULT_RELAY_HOST, sizeof(app.relay_host));
  app.relay_port = 8080;

  uint32_t bind_port = 0;
  for (int i = 1; i < argc; i++) {
    if (strcmp(argv[i], "--relay") == 0) app.is_relay = true;
    else if (strcmp(argv[i], "--http-port") == 0 && i+1 < argc) app.http_port = atoi(argv[++i]);
    else if (strcmp(argv[i], "--bind-port") == 0 && i+1 < argc) bind_port = atoi(argv[++i]);
    else if (strcmp(argv[i], "--name") == 0 && i+1 < argc) strncpy(app.node_name, argv[++i], 255);
    else if (strcmp(argv[i], "--relay-host") == 0 && i+1 < argc) strncpy(app.relay_host, argv[++i], 255);
    else if (strcmp(argv[i], "--relay-port") == 0 && i+1 < argc) app.relay_port = atoi(argv[++i]);
  }

  if (app.is_relay) return run_relay_server(&app);
  if (bind_port == 0) return 1;

  app.loop = uv_default_loop();
  uv_tcp_init(app.loop, &app.registration_client);
  struct sockaddr_in raddr; uv_ip4_addr(app.relay_host, app.relay_port, &raddr);
  app.registration_connect.data = &app;
  uv_tcp_connect(&app.registration_connect, &app.registration_client, (struct sockaddr*)&raddr, on_registration_connect);

  udx_init(app.loop, &app.udx, NULL);
  udx_socket_init(&app.udx, &app.socket, NULL);
  struct sockaddr_in baddr; uv_ip4_addr("0.0.0.0", bind_port, &baddr);
  udx_socket_bind(&app.socket, (struct sockaddr *)&baddr, 0);
  udx_stream_init(&app.udx, &app.stream, app.local_id, NULL, NULL);
  udx_stream_read_start(&app.stream, on_read);

  uv_timer_init(app.loop, &app.heartbeat_timer);
  app.heartbeat_timer.data = &app;
  uv_timer_start(&app.heartbeat_timer, on_heartbeat_tick, 1000, HEARTBEAT_INTERVAL_MS);

  uv_timer_init(app.loop, &app.discovery_timer);
  app.discovery_timer.data = &app;
  uv_timer_start(&app.discovery_timer, on_discovery_tick, 1000, DISCOVERY_INTERVAL_MS);

  printf("mesh v2 node %s active bind=%u\n", app.node_name, bind_port);
  fflush(stdout);
  return uv_run(app.loop, UV_RUN_DEFAULT);
}
