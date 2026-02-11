# Skyline + Mocking Bird Deployment Guide

Deploy Skyline MCP configured with Mocking Bird mock APIs for ChatGPT web integration.

## What This Does

1. **Deploys Mocking Bird** - Mock API server with 6 different protocol types
2. **Deploys Skyline** - MCP server configured to use Mocking Bird APIs
3. **Random subdomain** - Uses unpublished `<random>.example.com` for security
4. **Bearer token auth** - Secure authentication without exposing URL
5. **CORS enabled** - Works with ChatGPT web interface

## Prerequisites

- k3s cluster with Traefik ingress
- Wildcard DNS for `*.example.com` pointing to your cluster
- Wildcard TLS cert (or Let's Encrypt via cert-manager)
- `kubectl` configured with access to the cluster

## Quick Deploy

```bash
cd ~/code/skyline-mcp/k8s
./deploy-mockingbird.sh
```

This will:
- Generate a random subdomain (e.g., `f4a8c2e1b9d7f3e6a8c2b4d7e9f1a3c5.example.com`)
- Generate a secure bearer token (64 hex chars)
- Deploy Mocking Bird to `mockingbird` namespace
- Deploy Skyline to `skyline` namespace
- Configure Skyline to use Mocking Bird APIs
- Save credentials to `~/.skyline/mockingbird-credentials.txt`

## What You Get

### Mock APIs Available via Skyline:

| API Name | Protocol | Auth | Operations |
|----------|----------|------|------------|
| **pets-openapi** | OpenAPI 3.x | None | List, Create, Get, Update, Delete pets |
| **dinosaurs-swagger** | Swagger 2.0 | Bearer: `MOCK_DINO_TOKEN` | List, Create, Get, Update, Delete dinosaurs |
| **plants-soap** | WSDL/SOAP | Bearer: `MOCK_TOKEN` | SOAP operations for plants |
| **cars-graphql** | GraphQL | Basic: `graphql-user` / `MOCK_GRAPHQL_PASS` | Query and mutate cars |
| **movies-odata** | OData v4 | None | OData operations for movies |
| **calculator-jsonrpc** | JSON-RPC | None | Add, subtract, multiply, divide |

### Skyline Tools Generated:

Skyline automatically generates MCP tools from these APIs:
- **~50 tools total** (depends on API spec complexity)
- Full CRUD operations where supported
- GraphQL CRUD grouping enabled (reduced tool count)
- Proper authentication forwarding

## Using with ChatGPT

After deployment, you'll receive:

```
URL: https://<random>.example.com/mcp
Bearer Token: <64-char-hex-token>
```

### Configure ChatGPT:

1. Go to https://chatgpt.com/
2. Settings → Beta Features
3. Enable "MCP Servers" (if available)
4. Add server:
   - **URL:** `https://<random>.example.com/mcp`
   - **Auth:** Bearer token
   - **Token:** `<your-bearer-token>`

### Try These Prompts:

- "List all available pets"
- "Create a new pet named Fluffy who is a cat"
- "Add 15 and 27 using the calculator"
- "Query all cars with the color red"
- "Create a dinosaur named Rex"

## Manual Deployment Steps

If you prefer manual control:

### 1. Deploy Mocking Bird

```bash
KUBECONFIG=~/.kube/config kubectl apply -f ~/code/mocking-bird/k8s/deployment.yaml
```

Wait for ready:
```bash
KUBECONFIG=~/.kube/config kubectl wait --for=condition=available --timeout=120s deployment/mockingbird -n mockingbird
```

### 2. Generate Credentials

```bash
RANDOM_SUBDOMAIN=$(openssl rand -hex 16)
BEARER_TOKEN=$(openssl rand -hex 32)

echo "Subdomain: ${RANDOM_SUBDOMAIN}.example.com"
echo "Token: $BEARER_TOKEN"
```

### 3. Deploy Skyline

```bash
sed "s/REPLACE_WITH_RANDOM_TOKEN/${BEARER_TOKEN}/" ~/code/skyline-mcp/k8s/mockingbird-deployment.yaml | \
  sed "s/RANDOM_SUBDOMAIN/${RANDOM_SUBDOMAIN}/" | \
  KUBECONFIG=~/.kube/config kubectl apply -f -
```

Wait for ready:
```bash
KUBECONFIG=~/.kube/config kubectl wait --for=condition=available --timeout=180s deployment/skyline-mockingbird -n skyline
```

## Testing

### Test Mocking Bird Directly

```bash
# From inside the cluster
kubectl run -it --rm test-mockingbird --image=curlimages/curl --restart=Never -- \
  curl -s http://mockingbird.mockingbird.svc.cluster.local:9999/openapi/pets
```

### Test Skyline Endpoint

```bash
curl -X POST https://<random>.example.com/mcp \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
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

### List Tools

```bash
curl -X POST https://<random>.example.com/mcp \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -H "Mcp-Session-Id: test-session-123" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
  }' | jq '.result.tools[] | .name'
```

## Monitoring

### Check Pod Status

```bash
# Mocking Bird
KUBECONFIG=~/.kube/config kubectl get pods -n mockingbird

# Skyline
KUBECONFIG=~/.kube/config kubectl get pods -n skyline
```

### View Logs

```bash
# Mocking Bird logs
KUBECONFIG=~/.kube/config kubectl logs -f deployment/mockingbird -n mockingbird

# Skyline logs
KUBECONFIG=~/.kube/config kubectl logs -f deployment/skyline-mockingbird -n skyline
```

### Resource Usage

```bash
KUBECONFIG=~/.kube/config kubectl top pods -n mockingbird
KUBECONFIG=~/.kube/config kubectl top pods -n skyline
```

## Troubleshooting

### Mocking Bird Not Ready

```bash
# Check events
KUBECONFIG=~/.kube/config kubectl describe pod -n mockingbird | grep -A 10 Events

# Check if service is responding
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v http://mockingbird.mockingbird.svc.cluster.local:9999/openapi/pets
```

### Skyline Can't Reach Mocking Bird

```bash
# Test DNS resolution from Skyline pod
SKYLINE_POD=$(kubectl get pod -n skyline -l app=skyline -o jsonpath='{.items[0].metadata.name}')
kubectl exec -it $SKYLINE_POD -n skyline -- nslookup mockingbird.mockingbird.svc.cluster.local
```

### Ingress Not Working

```bash
# Check ingress status
KUBECONFIG=~/.kube/config kubectl get ingress -n skyline
KUBECONFIG=~/.kube/config kubectl describe ingress skyline-mockingbird -n skyline

# Check Traefik logs
KUBECONFIG=~/.kube/config kubectl logs -n kube-system -l app.kubernetes.io/name=traefik
```

### 401 Unauthorized

- Verify bearer token matches between Secret and your request
- Check if Authorization header is properly formatted: `Bearer <token>`
- View Skyline logs for auth attempts

### CORS Errors

- Ensure Origin header is being sent
- Check Skyline logs for CORS-related errors
- Verify Traefik isn't stripping headers

## Updating

### Update Mocking Bird

```bash
KUBECONFIG=~/.kube/config kubectl rollout restart deployment/mockingbird -n mockingbird
```

### Update Skyline

```bash
KUBECONFIG=~/.kube/config kubectl rollout restart deployment/skyline-mockingbird -n skyline
```

### Rotate Bearer Token

```bash
NEW_TOKEN=$(openssl rand -hex 32)
KUBECONFIG=~/.kube/config kubectl create secret generic skyline-auth \
  --from-literal=bearer-token="$NEW_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f - -n skyline

KUBECONFIG=~/.kube/config kubectl rollout restart deployment/skyline-mockingbird -n skyline

echo "New token: $NEW_TOKEN"
```

## Cleanup

```bash
# Remove Skyline
KUBECONFIG=~/.kube/config kubectl delete namespace skyline

# Remove Mocking Bird
KUBECONFIG=~/.kube/config kubectl delete namespace mockingbird
```

## Security Notes

- **Random subdomain** provides security through obscurity
- **Bearer token** is the real security mechanism
- **Never commit tokens** to git or share publicly
- **Rotate tokens regularly** (monthly recommended)
- **Wildcard DNS** handles routing without exposing endpoints
- **Rate limiting** prevents abuse (100 req/min default)

## Architecture

```
ChatGPT Web (https://chatgpt.com)
    ↓
    ↓ HTTPS + Bearer Token
    ↓
Random Subdomain (*.example.com)
    ↓
    ↓ Traefik Ingress (TLS termination)
    ↓
Skyline MCP Server (skyline namespace)
    ↓
    ↓ HTTP (internal cluster)
    ↓
Mocking Bird (mockingbird namespace)
    ↓
    ↓ Mock data responses
    ↓
Back to ChatGPT via Skyline
```

## Support

- Skyline: https://github.com/emadomedher/skyline-mcp
- Mocking Bird: https://github.com/emadomedher/mocking-bird
- MCP Spec: https://modelcontextprotocol.io
