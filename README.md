# proj_listas

App de listas con modo *versus*, listas colectivas y panel de administración.
Escrita en Go (chi + templates HTML). Funciona con SQLite local o PostgreSQL
(Vercel Postgres) con el mismo código.

## Estructura

- `main.go` — servidor HTTP (local: `go run .`, puerto 7010 con SQLite en `./data/listas.db`; en Vercel: el mismo binario, Postgres vía `DATABASE_URL`)
- `internal/server` — construcción del router; incluye embebidos `templates/`, `static/` y `dominios.txt` (go:embed)
- `internal/db` — conexión: `DATABASE_URL` (Postgres) > `LIBSQL_URL` (Turso) > SQLite local (`DB_PATH`)
- `internal/handlers`, `internal/auth`, `internal/models` — lógica de la app

La SQL usa placeholders `$1, $2, ...` (válidos en SQLite y Postgres) y
`INSERT ... RETURNING id` en lugar de `LastInsertId`.

## Desarrollo local

```sh
go run .          # http://localhost:7010 (SQLite local, migra automática)
```

Usuario admin inicial: `admin` / `admin` (cambia la contraseña al primer login).

## Vercel + Postgres

1. Crear el proyecto y la base:

   ```sh
   vercel link
   vercel postgres create   # o desde el dashboard: Storage → Postgres
   ```

   Esto expone `DATABASE_URL` (con `POSTGRES_URL` como alias) como variable de
   entorno del proyecto. La app detecta `DATABASE_URL` y usa Postgres
   automáticamente; las migraciones se ejecutan solas en cada cold start.

2. Desplegar:

   ```sh
   vercel deploy --prod
   ```

## Docker

```sh
docker compose up -d --build
```

Los templates/estáticos/dominios van embebidos en el binario; el volumen
`./data` persiste la base SQLite local. `DOMAINS_PATH` permite sobrescribir
`dominios.txt` con un archivo montado.
