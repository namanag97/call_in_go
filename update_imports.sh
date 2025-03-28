#!/bin/bash

echo "Updating import paths..."

# Update go.mod first
echo "Updating go.mod..."
sed -i '' 's|github.com/namanag97/call_in_go.git|github.com/namanag97/call_in_go/call-processor|g' call-processor/go.mod

# Update all Go files
echo "Updating Go files..."
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/your-org/call-processing|github.com/namanag97/call_in_go/call-processor/internal|g' {} +

# Update any internal package references
echo "Updating internal package references..."
find call-processor -type f -name "*.go" -exec sed -i '' 's|internaldomain|internal/domain|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalevent|internal/event|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalrepository|internal/repository|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalworker|internal/worker|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalstorage|internal/storage|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internaltranscription|internal/transcription|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalanalysis|internal/analysis|g' {} +
find call-processor -type f -name "*.go" -exec sed -i '' 's|internalingestion|internal/ingestion|g' {} +

# Update any remaining incorrect paths
echo "Updating remaining incorrect paths..."
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/internal/internal|github.com/namanag97/call_in_go/call-processor/internal|g' {} +

echo "Import paths updated successfully!" 