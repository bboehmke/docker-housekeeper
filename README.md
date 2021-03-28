# Docker Housekeeper

Features:
* Database initialization
    * Create Database
    * Create User
    * add PG extensions
* Scheduled backup of database and data directories

## Usage

Only database initialization:
```yaml
services:
  db_init:
    image: bboehmke/docker-housekeeper
    environment:
      DB_HOST: host_name
      DB_ROOT_PASSWORD: password
      DB_DATABASE: database_to_create
      DB_USER_NAME: user_to_create
      DB_USER_PASSWORD: user_password
```

Backup 2 directories:
```yaml
services:
  db_init:
    image: bboehmke/docker-housekeeper
    volumes:
      - ./data/A/:/data/A/
      - ./data/B/:/data/B/
      - ./backup/:/backup/
    environment:
      BACKUP_DATA_DIR: "/data/A,/data/B"
```

Initialize database and scheduled backup (including directories):
```yaml
services:
  db_init:
    image: bboehmke/docker-housekeeper
    volumes:
      - ./data/A/:/data/A/
      - ./data/B/:/data/B/
      - ./backup/:/backup/
    environment:
      DB_HOST: host_name
      DB_ROOT_PASSWORD: password
      DB_DATABASE: database_to_create
      DB_USER_NAME: user_to_create
      DB_USER_PASSWORD: user_password

      BACKUP_DATABASE: "true"
      BACKUP_DATA_DIR: "/data/A,/data/B"
```

## Available Configuration Parameters

The configuration is done via environment variables.

### Database

- **DB_HOST**: Hostname of database server
- **DB_PORT**: Port of database server (Default: 5432)
- **DB_ROOT_PASSWORD**: Password of root account
- **DB_ROOT_USER**: Name of root account (Default: postgres)
- **DB_DATABASE**: Database to create
- **DB_USER_NAME**: User to create with access to `DB_DATABASE`
- **DB_USER_PASSWORD**: Password of `DB_USER_NAME`
- **DB_PG_EXTENSIONS**: List of postgres extensions

### Backup

- **BACKUP_DATABASE**: True if database should be part of backup
- **BACKUP_DATA_DIR**: List of directories to back up (Separated by ",")
- **BACKUP_DATA_EXCLUDE**: List of directories to exclude from backup (Separated by ",")
- **BACKUP_SCHEDULE**: [Cron expression](https://en.wikipedia.org/wiki/Cron) (Default: @daily)
- **BACKUP_STORAGE**: Storage location for backups
