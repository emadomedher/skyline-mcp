#!/bin/bash
# Start skyline, send tools/list request, get response
./bin/mcp-api-bridge --config ./config.myka-gitlab.yaml --transport stdio 2>&1 | while read line; do
  echo "$line" | jq -r '.result.tools[]?.name' 2>/dev/null | head -20
done &
PID=$!
sleep 5
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' 
sleep 2
kill $PID 2>/dev/null
