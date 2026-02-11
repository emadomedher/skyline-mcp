#!/bin/bash
set -e

# Generate random subdomain (32 chars hex)
RANDOM_SUBDOMAIN=$(openssl rand -hex 16)
echo "📍 Random subdomain: ${RANDOM_SUBDOMAIN}.medher.online"

# Generate bearer token (64 chars hex)
BEARER_TOKEN=$(openssl rand -hex 32)
echo "🔑 Bearer token: $BEARER_TOKEN"

# Save credentials
mkdir -p ~/.skyline
cat > ~/.skyline/mockingbird-credentials.txt << EOF
Skyline with Mocking Bird Deployment
====================================
URL: https://${RANDOM_SUBDOMAIN}.medher.online/mcp
Bearer Token: ${BEARER_TOKEN}
Deployed: $(date)

To use with ChatGPT:
1. Go to https://chatgpt.com/ → Settings → Beta Features
2. Enable MCP Servers
3. Add server:
   - URL: https://${RANDOM_SUBDOMAIN}.medher.online/mcp
   - Auth: Bearer token
   - Token: ${BEARER_TOKEN}

Mock APIs Available:
- pets-openapi: OpenAPI 3.x (Pets CRUD)
- dinosaurs-swagger: Swagger 2.0 (Dinosaurs CRUD, bearer token required)
- plants-soap: WSDL/SOAP (Plants, bearer token required)
- cars-graphql: GraphQL (Cars, basic auth required)
- movies-odata: OData v4 (Movies CRUD)
- calculator-jsonrpc: JSON-RPC (Calculator operations)
EOF

chmod 600 ~/.skyline/mockingbird-credentials.txt
echo "💾 Credentials saved to: ~/.skyline/mockingbird-credentials.txt"

# Deploy Mocking Bird first
echo ""
echo "🐦 Deploying Mocking Bird..."
KUBECONFIG=~/.kube/config kubectl apply -f ~/code/mocking-bird/k8s/deployment.yaml

# Wait for Mocking Bird to be ready
echo "⏳ Waiting for Mocking Bird to be ready..."
KUBECONFIG=~/.kube/config kubectl wait --for=condition=available --timeout=120s deployment/mockingbird -n mockingbird

# Update Skyline deployment with random subdomain and bearer token
echo ""
echo "🚀 Deploying Skyline with Mocking Bird config..."
sed "s/REPLACE_WITH_RANDOM_TOKEN/${BEARER_TOKEN}/" ~/code/skyline-mcp/k8s/mockingbird-deployment.yaml | \
  sed "s/RANDOM_SUBDOMAIN/${RANDOM_SUBDOMAIN}/" | \
  KUBECONFIG=~/.kube/config kubectl apply -f -

# Wait for Skyline to be ready
echo "⏳ Waiting for Skyline to be ready..."
KUBECONFIG=~/.kube/config kubectl wait --for=condition=available --timeout=180s deployment/skyline-mockingbird -n skyline

echo ""
echo "✅ Deployment complete!"
echo ""
echo "📋 Summary:"
echo "  URL: https://${RANDOM_SUBDOMAIN}.medher.online/mcp"
echo "  Token: ${BEARER_TOKEN}"
echo ""
echo "🧪 Test the connection:"
echo "  curl -X POST https://${RANDOM_SUBDOMAIN}.medher.online/mcp \\"
echo "    -H 'Authorization: Bearer ${BEARER_TOKEN}' \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -H 'Accept: application/json' \\"
echo "    -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-11-25\",\"capabilities\":{},\"clientInfo\":{\"name\":\"test\",\"version\":\"1.0\"}}}'"
echo ""
echo "📄 Full credentials at: ~/.skyline/mockingbird-credentials.txt"
