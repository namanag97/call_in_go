#!/bin/bash

# This script drops and recreates the specified databases
# Exclude system databases (postgres, template0, template1)

echo "Dropping and recreating databases..."

# Drop and recreate call-fi database
psql -c "DROP DATABASE IF EXISTS \"call-fi\";" postgres
psql -c "CREATE DATABASE \"call-fi\";" postgres
echo "Reset call-fi database"

# Drop and recreate call_analytics_test database
psql -c "DROP DATABASE IF EXISTS call_analytics_test;" postgres
psql -c "CREATE DATABASE call_analytics_test;" postgres
echo "Reset call_analytics_test database"

# Drop and recreate call_processing database
psql -c "DROP DATABASE IF EXISTS call_processing;" postgres
psql -c "CREATE DATABASE call_processing;" postgres
echo "Reset call_processing database"

echo "All specified databases have been reset!" 