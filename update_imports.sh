#!/bin/bash

# Update go.mod
sed -i '' 's|github.com/namanag97/call_in_go.git|github.com/namanag97/call_in_go/call-processor|g' call-processor/go.mod

# Update all Go files
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/your-org/call-processing|github.com/namanag97/call_in_go/call-processor|g' {} +

echo "Import paths updated successfully!" 