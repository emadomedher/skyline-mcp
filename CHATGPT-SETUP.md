# Secure Skyline Setup for ChatGPT Web

## Overview
This guide shows how to securely expose Skyline MCP to ChatGPT's web interface (https://chatgpt.com/).

## Security Features
- ✅ **TLS/HTTPS** via Traefik + Let's Encrypt
- ✅ **Bearer token authentication** (required for API access)
- ✅ **Rate limiting** (100 requests/min, burst 50)
- ✅ **Security headers** (HSTS, X-Frame-Options, etc.)
- ✅ **GitLab credentials** stored in k8s Secret

## Prerequisites
- k3s cluster with Traefik ingress
- Domain: `*.medher.online` (already configured)
- cert-manager for Let's Encrypt (or manual TLS cert)

## Step 1: Generate Bearer Token

```bash
# Generate a secure random token (keep this safe!)
BEARER_TOKEN=$(openssl rand -hex 32)
echo "Your Skyline bearer token: $BEARER_TOKEN"

# Save it somewhere secure - you'll need it for ChatGPT configuration
echo "$BEARER_TOKEN" > ~/skyline-bearer-token.txt
chmod 600 ~/skyline-bearer-token.txt
```

**Example token:** `022793bd27145b4bd443b5e75add54ecb2231e229195dce465ac2bc3c04df578`

## Step 2: Update Deployment

Edit `k8s/deployment.yaml` and replace the bearer-token line:

```yaml
stringData:
  bearer-token: "022793bd27145b4bd443b5e75add54ecb2231e229195dce465ac2bc3c04df578"
```

## Step 3: Deploy to Kubernetes

```bash
cd ~/code/skyline-mcp

# Apply the deployment
KUBECONFIG=~/.kube/config kubectl apply -f k8s/deployment.yaml

# Wait for deployment to be ready
KUBECONFIG=~/.kube/config kubectl rollout status deployment/skyline -n skyline

# Verify it's running
KUBECONFIG=~/.kube/config kubectl get pods -n skyline
```

## Step 4: Test the Endpoint

```bash
# Get your bearer token
BEARER_TOKEN=$(cat ~/skyline-bearer-token.txt)

# Test locally first
curl -X POST https://skyline.medher.online/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $BEARER_TOKEN" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {},
      "clientInfo": {"name": "test", "version": "1.0"}
    }
  }'
```

Expected response:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "capabilities": {...},
    "protocolVersion": "2025-11-25",
    "serverInfo": {
      "name": "Skyline MCP",
      "version": "0.1.0"
    }
  }
}
```

## Step 5: Configure ChatGPT

1. **Go to ChatGPT Settings:**
   - Visit https://chatgpt.com/
   - Click your profile → Settings → Beta features
   - Enable "Use MCP servers" (if available)

2. **Add Skyline Server:**
   - Click "Add MCP Server"
   - **Server URL:** `https://skyline.medher.online/mcp`
   - **Authentication:** Bearer token
   - **Token:** `022793bd27145b4bd443b5e75add54ecb2231e229195dce465ac2bc3c04df578`

3. **Test Connection:**
   - ChatGPT will verify the connection
   - You should see "78 tools available" (GitLab CRUD operations)

4. **Start Using:**
   - Try: "List my GitLab projects"
   - Try: "Create a new issue in project X"

## Security Notes

### Bearer Token Protection
- Never commit the token to git
- Store in k8s Secret (already done in deployment)
- Rotate regularly: `kubectl set env deployment/skyline BEARER_TOKEN="$(openssl rand -hex 32)" -n skyline`

### Rate Limiting
Current limits (adjust in deployment.yaml):
- **Average:** 100 requests/min
- **Burst:** 50 requests

To adjust:
```yaml
rateLimit:
  average: 200    # Increase if needed
  burst: 100
  period: 1m
```

### TLS Certificate
- **Auto-renewal:** Let's Encrypt via cert-manager
- **Wildcard cert:** `*.medher.online` (if already configured)
- **Manual cert:** Replace `cert-manager.io/cluster-issuer` annotation

### Monitoring

Check logs:
```bash
KUBECONFIG=~/.kube/config kubectl logs -f deployment/skyline -n skyline
```

Check resource usage:
```bash
KUBECONFIG=~/.kube/config kubectl top pod -n skyline
```

## Troubleshooting

### Connection Refused
```bash
# Check if pod is running
KUBECONFIG=~/.kube/config kubectl get pods -n skyline

# Check service
KUBECONFIG=~/.kube/config kubectl get svc -n skyline

# Check ingress
KUBECONFIG=~/.kube/config kubectl get ingress -n skyline
```

### 401 Unauthorized
- Verify bearer token matches in Secret and ChatGPT config
- Check if token has extra spaces or newlines

### Rate Limit Hit
- Check Traefik middleware logs
- Increase rate limits if legitimate usage

### Tools Not Loading
- Check Skyline logs for GitLab connection errors
- Verify GitLab token in Secret is valid
- Test GraphQL endpoint: `curl https://lab.medher.online/api/graphql`

## Alternative: Cloudflare Tunnel (Zero Trust)

If you prefer not exposing via ingress:

```bash
# Install cloudflared
curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 -o /usr/local/bin/cloudflared
chmod +x /usr/local/bin/cloudflared

# Create tunnel
cloudflared tunnel create skyline-mcp

# Configure tunnel
cat > ~/.cloudflared/config.yml << EOF
tunnel: <TUNNEL_ID>
credentials-file: /root/.cloudflared/<TUNNEL_ID>.json

ingress:
  - hostname: skyline.medher.online
    service: http://localhost:8089
    originRequest:
      httpHostHeader: skyline.medher.online
  - service: http_status:404
EOF

# Run tunnel
cloudflared tunnel run skyline-mcp
```

Then configure DNS in Cloudflare dashboard.

## Next Steps

1. **Monitor usage** - Check logs regularly
2. **Rotate tokens** - Monthly token rotation recommended
3. **Add more APIs** - Extend config.json with other services
4. **Setup alerts** - Configure k8s monitoring (Prometheus, Grafana)

## Support

- Skyline docs: https://skyline.projex.cc
- MCP spec: https://modelcontextprotocol.io
- GitHub: https://github.com/emadomedher/skyline-mcp
