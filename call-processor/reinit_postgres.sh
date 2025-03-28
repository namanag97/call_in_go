#!/bin/bash

# Stop PostgreSQL service if running
echo "Stopping PostgreSQL service..."
brew services stop postgresql@14

# Remove data directory if it exists
echo "Removing old data directory..."
rm -rf /opt/homebrew/var/postgresql@14

# Initialize a new PostgreSQL database
echo "Initializing new PostgreSQL database..."
/opt/homebrew/opt/postgresql@14/bin/initdb -D /opt/homebrew/var/postgresql@14

# Start PostgreSQL service
echo "Starting PostgreSQL service..."
brew services start postgresql@14

# Wait for PostgreSQL to start up
echo "Waiting for PostgreSQL to start..."
sleep 5

# Check if PostgreSQL is running
pg_isready
if [ $? -eq 0 ]; then
  echo "PostgreSQL is now running and ready for use!"
else
  echo "PostgreSQL failed to start properly. Check the logs at /opt/homebrew/var/log/postgresql@14.log"
  exit 1
fi

# Create postgres role if it doesn't exist
echo "Creating postgres role..."
psql -U $(whoami) -d postgres -c "DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'postgres') THEN
    CREATE ROLE postgres WITH LOGIN SUPERUSER;
  END IF;
END
\$\$;"

# Create project database
echo "Creating database for current project..."
psql -U $(whoami) -d postgres -c "CREATE DATABASE go_project;"

echo "PostgreSQL has been reinitialized with clean databases for your current project!" 