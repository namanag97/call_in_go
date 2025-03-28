# Go Project

A Go-based project for call processing.

## Setup

### Database Setup

To initialize the PostgreSQL database for this project, run:

```bash
./reinit_postgres.sh
```

This script will:
1. Stop any running PostgreSQL service
2. Remove existing data directory
3. Initialize a new PostgreSQL database
4. Start the PostgreSQL service
5. Create a database called `go_project`

## Project Structure

- `call-processor/` - Contains the call processing application 