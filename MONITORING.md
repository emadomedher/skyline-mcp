# Skyline Monitoring & Observability

Skyline includes comprehensive monitoring and logging capabilities for tracking API usage, performance metrics, and audit trails.

## Features

✅ **Structured Audit Logging** - All API calls logged to SQLite with full context
✅ **Real-time Metrics** - Prometheus-compatible metrics endpoint
✅ **Admin Dashboard** - Web UI for viewing metrics and audit logs
✅ **Performance Tracking** - Request duration histograms and statistics
✅ **Profile Analytics** - Per-profile request counting and filtering

---

## Quick Start

### 1. Start Skyline with Monitoring

```bash
export CONFIG_SERVER_KEY="base64:$(openssl rand -base64 32)"
go run ./cmd/skyline --listen :9190
```

The audit database is automatically created at `./skyline-audit.db`

### 2. Access Admin Dashboard

Open [http://localhost:9190/admin/](http://localhost:9190/admin/)

The dashboard displays:
- Total requests and success rate
- Active WebSocket connections
- Average response time
- Recent API calls (last 50)
- Per-profile statistics
- Auto-refreshes every 10 seconds

---

## Monitoring Endpoints

### `/admin/metrics` - Prometheus Metrics

Returns metrics in Prometheus text format for scraping:

```bash
curl http://localhost:9190/admin/metrics
```

**Metrics Available:**

| Metric | Type | Description |
|--------|------|-------------|
| `skyline_requests_total` | counter | Total number of requests |
| `skyline_requests_success_total` | counter | Total successful requests |
| `skyline_requests_failed_total` | counter | Total failed requests |
| `skyline_requests_by_profile_total{profile}` | counter | Requests per profile |
| `skyline_requests_by_tool_total{tool}` | counter | Requests per tool |
| `skyline_connections_active` | gauge | Current active connections |
| `skyline_connections_total` | counter | Total connections |
| `skyline_request_duration_milliseconds` | histogram | Request duration distribution |
| `skyline_uptime_seconds` | counter | Uptime in seconds |

**Example Output:**

```
# HELP skyline_requests_total Total number of requests
# TYPE skyline_requests_total counter
skyline_requests_total 1523

# HELP skyline_requests_success_total Total number of successful requests
# TYPE skyline_requests_success_total counter
skyline_requests_success_total 1489

# HELP skyline_connections_active Number of active WebSocket connections
# TYPE skyline_connections_active gauge
skyline_connections_active 3
```

### `/admin/audit` - Audit Log Query

Query audit logs with filters:

```bash
# Get last 50 events
curl http://localhost:9190/admin/audit?limit=50

# Filter by profile
curl http://localhost:9190/admin/audit?profile=production&limit=100

# Filter by event type
curl http://localhost:9190/admin/audit?event_type=execute&limit=20

# Filter by tool name
curl http://localhost:9190/admin/audit?tool_name=petstore__getPetById&limit=10
```

**Query Parameters:**

| Parameter | Description | Default |
|-----------|-------------|---------|
| `profile` | Filter by profile name | all |
| `event_type` | Filter by event type (`execute`, `connect`, `disconnect`, `error`) | all |
| `tool_name` | Filter by tool name | all |
| `limit` | Maximum number of events to return | 100 |

**Response Format:**

```json
{
  "events": [
    {
      "id": 1234,
      "timestamp": "2026-02-06T17:30:45Z",
      "profile": "production",
      "event_type": "execute",
      "tool_name": "petstore__getPetById",
      "arguments": {
        "petId": "123"
      },
      "duration_ms": 234,
      "status_code": 200,
      "success": true,
      "client_addr": "127.0.0.1:54321"
    }
  ],
  "count": 1
}
```

### `/admin/stats` - Aggregated Statistics

Get aggregated statistics for a time period:

```bash
# Get stats for last 24 hours (default)
curl http://localhost:9190/admin/stats

# Get stats for specific profile
curl http://localhost:9190/admin/stats?profile=production

# Get stats since specific time
curl "http://localhost:9190/admin/stats?since=2026-02-06T00:00:00Z"
```

**Response Format:**

```json
{
  "audit_stats": {
    "total_requests": 1523,
    "successful_requests": 1489,
    "failed_requests": 34,
    "error_rate": 2.23,
    "avg_duration_ms": 187,
    "max_duration_ms": 2341,
    "min_duration_ms": 45
  },
  "metrics_snapshot": {
    "total_requests": 1523,
    "success_requests": 1489,
    "failed_requests": 34,
    "active_connections": 3,
    "total_connections": 15,
    "avg_duration_ms": 187.45,
    "profile_requests": {
      "production": 1200,
      "staging": 323
    },
    "tool_requests": {
      "petstore__getPetById": 450,
      "petstore__updatePet": 123
    },
    "uptime_seconds": 86400.5
  },
  "period": {
    "since": "2026-02-05T17:30:00Z",
    "until": "2026-02-06T17:30:00Z"
  }
}
```

---

## Prometheus Integration

### 1. Configure Prometheus

Add Skyline to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'skyline'
    static_configs:
      - targets: ['localhost:9190']
    metrics_path: '/admin/metrics'
    scrape_interval: 15s
```

### 2. Start Prometheus

```bash
prometheus --config.file=prometheus.yml
```

### 3. Create Grafana Dashboard

Example Grafana queries:

```promql
# Request rate (per second)
rate(skyline_requests_total[5m])

# Error rate (percentage)
rate(skyline_requests_failed_total[5m]) / rate(skyline_requests_total[5m]) * 100

# Average request duration (milliseconds)
rate(skyline_request_duration_milliseconds_sum[5m]) / rate(skyline_request_duration_milliseconds_count[5m])

# P95 latency
histogram_quantile(0.95, rate(skyline_request_duration_milliseconds_bucket[5m]))

# Active connections
skyline_connections_active
```

---

## Audit Database

### Schema

The audit log is stored in SQLite at `./skyline-audit.db`:

```sql
CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME NOT NULL,
    profile TEXT NOT NULL,
    event_type TEXT NOT NULL,
    tool_name TEXT,
    arguments TEXT,  -- JSON
    duration_ms INTEGER,
    status_code INTEGER,
    success BOOLEAN NOT NULL,
    error_msg TEXT,
    client_addr TEXT,
    request_size INTEGER,
    response_size INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Direct SQL Queries

You can query the database directly:

```bash
sqlite3 skyline-audit.db

# Get failed requests in last hour
SELECT timestamp, profile, tool_name, error_msg
FROM audit_events
WHERE success = 0
  AND timestamp >= datetime('now', '-1 hour')
ORDER BY timestamp DESC;

# Get slowest requests
SELECT timestamp, profile, tool_name, duration_ms
FROM audit_events
WHERE event_type = 'execute'
ORDER BY duration_ms DESC
LIMIT 10;

# Count requests by profile
SELECT profile, COUNT(*) as total,
       SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as successful
FROM audit_events
WHERE event_type = 'execute'
GROUP BY profile;
```

### Export Audit Logs

Export to CSV:

```bash
sqlite3 -header -csv skyline-audit.db \
  "SELECT * FROM audit_events WHERE timestamp >= date('now', '-7 days')" \
  > audit_export.csv
```

Export to JSON:

```bash
sqlite3 -json skyline-audit.db \
  "SELECT * FROM audit_events WHERE timestamp >= date('now', '-7 days')" \
  > audit_export.json
```

---

## Performance Considerations

### Audit Log Size

The audit database grows over time. To manage size:

```bash
# Check database size
du -h skyline-audit.db

# Vacuum to reclaim space
sqlite3 skyline-audit.db "VACUUM;"

# Delete old entries (older than 30 days)
sqlite3 skyline-audit.db \
  "DELETE FROM audit_events WHERE timestamp < datetime('now', '-30 days');"
```

### Batch Writes

Audit events are buffered and written in batches:
- Default batch size: 100 events
- Default flush interval: 5 seconds
- Automatic flush on buffer full

This minimizes I/O overhead while ensuring events are persisted quickly.

### Metrics Memory Usage

Metrics are stored in memory with atomic operations:
- Counters: 8 bytes per metric
- Histograms: ~800 bytes (10 buckets)
- Per-profile/tool maps: dynamic allocation

Expected memory usage: ~1-5MB for typical workloads.

---

## Alerting Examples

### Prometheus Alerting Rules

```yaml
groups:
  - name: skyline_alerts
    rules:
      # High error rate
      - alert: SkylineHighErrorRate
        expr: rate(skyline_requests_failed_total[5m]) / rate(skyline_requests_total[5m]) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Skyline error rate above 5%"

      # Slow requests
      - alert: SkylineSlowRequests
        expr: histogram_quantile(0.95, rate(skyline_request_duration_milliseconds_bucket[5m])) > 5000
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "P95 latency above 5 seconds"

      # No active connections
      - alert: SkylineNoConnections
        expr: skyline_connections_active == 0
        for: 5m
        labels:
          severity: info
        annotations:
          summary: "No active WebSocket connections"
```

---

## Security Considerations

### Admin Endpoints

The admin endpoints (`/admin/*`) currently have **no authentication**.

**For production deployments**, add authentication:

1. **Reverse Proxy Authentication** (Recommended)

```nginx
location /admin/ {
    auth_basic "Skyline Admin";
    auth_basic_user_file /etc/nginx/.htpasswd;
    proxy_pass http://localhost:9190;
}
```

2. **IP Allowlist**

```nginx
location /admin/ {
    allow 10.0.0.0/8;
    allow 192.168.0.0/16;
    deny all;
    proxy_pass http://localhost:9190;
}
```

3. **VPN/Private Network**

Deploy Skyline on a private network and access admin endpoints through VPN only.

### Audit Data Privacy

The audit log contains:
- Tool names
- Request arguments (may include sensitive data)
- Client IP addresses

**Recommendations:**
- Regularly purge old audit entries
- Encrypt the audit database at rest
- Restrict access to `skyline-audit.db` file
- Consider redacting sensitive fields in arguments

---

## Troubleshooting

### Dashboard Not Loading

**Issue:** Admin dashboard shows loading spinner indefinitely

**Solution:**
```bash
# Check if endpoints are accessible
curl http://localhost:9190/admin/stats
curl http://localhost:9190/admin/audit?limit=1

# Check browser console for errors
# Open developer tools (F12) and check Console tab
```

### High Memory Usage

**Issue:** Metrics memory growing over time

**Solution:**
- Per-profile and per-tool metrics are unbounded
- Consider implementing a max map size or TTL for inactive entries
- Restart Skyline periodically to clear metrics

### Audit Database Locked

**Issue:** `database is locked` error

**Solution:**
```bash
# Check for other processes accessing the database
lsof skyline-audit.db

# Increase SQLite timeout (modify audit.go)
# Add: db.SetConnMaxLifetime(time.Second * 30)
```

### Missing Metrics

**Issue:** Some metrics not appearing

**Solution:**
- Ensure requests are actually being made
- Check that WebSocket connection is established
- Verify no errors in Skyline logs
- Try the `/admin/stats` endpoint to see raw data

---

## Future Enhancements

Planned features for future versions:

- [ ] Distributed tracing (OpenTelemetry integration)
- [ ] Log aggregation (Loki integration)
- [ ] Real-time WebSocket streaming of audit events
- [ ] Built-in authentication for admin endpoints
- [ ] Configurable retention policies
- [ ] Advanced filtering in admin dashboard
- [ ] Request/response payload logging (opt-in)
- [ ] Rate limiting per profile
- [ ] Cost tracking (API call pricing)
- [ ] SLA monitoring and alerts

---

## API Reference

See the full [Skyline API Documentation](API.md) for detailed endpoint specifications.

For questions or issues, please open an issue on GitHub.
