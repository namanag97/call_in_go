#!/bin/bash

echo "Updating import paths..."

# Update go.mod first
echo "Updating go.mod..."
sed -i '' 's|github.com/namanag97/call_in_go.git|github.com/namanag97/call_in_go/call-processor|g' call-processor/go.mod

# Update all Go files
echo "Updating Go files..."

# Update old organization imports
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/your-org/call-processing|github.com/namanag97/call_in_go/call-processor/internal|g' {} +

# Update incorrect internal package references
find call-processor -type f -name "*.go" -exec sed -i '' 's|internaldomain|internal/domain|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalevent|internal/event|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalrepository|internal/repository|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalworker|internal/worker|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalstorage|internal/storage|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internaltranscription|internal/transcription|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalanalysis|internal/analysis|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalingestion|internal/ingestion|g' {} +

# Update incorrect package paths without internal prefix
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/domain|github.com/namanag97/call_in_go/call-processor/internal/domain|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/event|github.com/namanag97/call_in_go/call-processor/internal/event|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/repository|github.com/namanag97/call_in_go/call-processor/internal/repository|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/worker|github.com/namanag97/call_in_go/call-processor/internal/worker|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/storage|github.com/namanag97/call_in_go/call-processor/internal/storage|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/transcription|github.com/namanag97/call_in_go/call-processor/internal/transcription|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/analysis|github.com/namanag97/call_in_go/call-processor/internal/analysis|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/ingestion|github.com/namanag97/call_in_go/call-processor/internal/ingestion|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/stt|github.com/namanag97/call_in_go/call-processor/internal/stt|g' {} +

# Update any remaining incorrect paths
echo "Updating remaining incorrect paths..."
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/internal/internal|github.com/namanag97/call_in_go/call-processor/internal|g' {} +

echo "Import paths updated successfully!" 