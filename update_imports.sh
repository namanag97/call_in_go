#!/bin/bash

echo "Updating import paths..."

# Update go.mod first
echo "Updating go.mod..."
sed -i '' 's|github.com/namanag97/call_in_go.git|github.com/namanag97/call_in_go/call-processor|g' call-processor/go.mod

# Update all Go files
echo "Updating Go files..."

# Update old organization imports
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/your-org/call-processing|github.com/namanag97/call_in_go/call-processor/internal|g' {} +

# Update incorrect internal package references (with no slashes)
echo "Updating internal package references..."
find call-processor -type f -name "*.go" -exec sed -i '' \
    -e 's|internalapi|internal/api|g' \
    -e 's|internaldomain|internal/domain|g' \
    -e 's|internalevent|internal/event|g' \
    -e 's|internalrepository|internal/repository|g' \
    -e 's|internalworker|internal/worker|g' \
    -e 's|internalstorage|internal/storage|g' \
    -e 's|internaltranscription|internal/transcription|g' \
    -e 's|internalanalysis|internal/analysis|g' \
    -e 's|internalingestion|internal/ingestion|g' \
    -e 's|internalstt|internal/stt|g' {} +

# Update incorrect package paths without internal prefix
echo "Updating package paths..."
find call-processor -type f -name "*.go" -exec sed -i '' \
    -e 's|github.com/namanag97/call_in_go/call-processor/api|github.com/namanag97/call_in_go/call-processor/internal/api|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/domain|github.com/namanag97/call_in_go/call-processor/internal/domain|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/event|github.com/namanag97/call_in_go/call-processor/internal/event|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/repository|github.com/namanag97/call_in_go/call-processor/internal/repository|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/worker|github.com/namanag97/call_in_go/call-processor/internal/worker|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/storage|github.com/namanag97/call_in_go/call-processor/internal/storage|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/transcription|github.com/namanag97/call_in_go/call-processor/internal/transcription|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/analysis|github.com/namanag97/call_in_go/call-processor/internal/analysis|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/ingestion|github.com/namanag97/call_in_go/call-processor/internal/ingestion|g' \
    -e 's|github.com/namanag97/call_in_go/call-processor/stt|github.com/namanag97/call_in_go/call-processor/internal/stt|g' {} +

# Update any remaining incorrect paths
echo "Cleaning up any duplicate internal paths..."
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/internal/internal|github.com/namanag97/call_in_go/call-processor/internal|g' {} +

# Update any remaining incorrect module paths
echo "Updating remaining module paths..."
find call-processor -type f -name "*.go" -exec sed -i '' 's|github.com/namanag97/call_in_go/call-processor/call-processor|github.com/namanag97/call_in_go/call-processor|g' {} +

echo "Import paths updated successfully!" 