package store

// This file holds the curated built-in service templates. Add new built-ins
// here; SeedBuiltins (in templates.go) inserts them on first run and is
// idempotent on Name, so additions land on the next Open of a fresh database.
//
// Dockerfiles are embedded so the later "create service" flow can write them
// into a service root without the user needing to author one. Datastore entries
// use Mode=="image" and carry an image name instead of a Dockerfile; image-pull
// deploy support lands in a follow-up, but the templates are representable now.

var builtinTemplates = []ServiceTemplate{
	{
		Name:        "Next.js",
		Description: "Production Next.js: multi-stage build served by `next start`.",
		Category:    "web",
		Icon:        "nextdotjs",
		Color:       "#000000",
		Mode:        "build",
		Port:        3000,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM node:20-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm ci --omit=dev

FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /app/node_modules ./node_modules
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/public ./public
COPY --from=builder /app/package.json ./package.json
COPY --from=builder /app/next.config.* ./
EXPOSE 3000
CMD ["npm", "start"]
`,
		EnvVars: `[{"key":"NODE_ENV","value":"production","scope":"runtime"},{"key":"HOSTNAME","value":"0.0.0.0","scope":"runtime"},{"key":"PORT","value":"3000","scope":"runtime"}]`,
	},
	{
		Name:        "Node.js",
		Description: "Generic Node.js service run via `npm start`.",
		Category:    "language",
		Icon:        "nodedotjs",
		Color:       "#5FA04E",
		Mode:        "build",
		Port:        3000,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY package*.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
`,
		EnvVars: `[{"key":"NODE_ENV","value":"production","scope":"runtime"},{"key":"HOSTNAME","value":"0.0.0.0","scope":"runtime"}]`,
	},
	{
		Name:        "FastAPI / Uvicorn",
		Description: "ASGI Python service served by Gunicorn with Uvicorn workers.",
		Category:    "web",
		Icon:        "fastapi",
		Color:       "#009688",
		Mode:        "build",
		Port:        8000,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM python:3.12-slim
WORKDIR /app
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["gunicorn", "app.main:app", "-k", "uvicorn.workers.UvicornWorker", "-w", "2", "-b", "0.0.0.0:8000"]
`,
		EnvVars: `[{"key":"PYTHONUNBUFFERED","value":"1","scope":"runtime"},{"key":"UVICORN_HOST","value":"0.0.0.0","scope":"runtime"},{"key":"UVICORN_PORT","value":"{{draft.service_port}}","scope":"runtime"}]`,
	},
	{
		Name:        "Flask",
		Description: "Lightweight WSGI Python service served by Gunicorn.",
		Category:    "web",
		Icon:        "flask",
		Color:       "#000000",
		Mode:        "build",
		Port:        5000,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM python:3.12-slim
WORKDIR /app
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 5000
CMD ["gunicorn", "app:app", "-w", "2", "-b", "0.0.0.0:5000"]
`,
		EnvVars: `[{"key":"FLASK_APP","value":"app.py","scope":"runtime"},{"key":"PYTHONUNBUFFERED","value":"1","scope":"runtime"}]`,
	},
	{
		Name:        "Vite",
		Description: "Production Vite SPA: build then serve static assets with nginx.",
		Category:    "web",
		Icon:        "vite",
		Color:       "#646CFF",
		Mode:        "build",
		Port:        5173,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine AS runner
RUN sed -i 's/listen[[:space:]]*80;/listen 5173;/' /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 5173
CMD ["nginx", "-g", "daemon off;"]
`,
		EnvVars: `[{"key":"NODE_ENV","value":"production","scope":"runtime"}]`,
	},
	{
		Name:        "PostgreSQL",
		Description: "Relational database. Runs from the official image.",
		Category:    "datastore",
		Icon:        "postgresql",
		Color:       "#4169E1",
		Mode:        "image",
		Image:       "postgres:16-alpine",
		Port:        5432,
		EnvVars: `[{"key":"POSTGRES_USER","value":"{{draft.db_user}}","scope":"runtime"},{"key":"POSTGRES_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"POSTGRES_DB","value":"{{draft.db_name}}","scope":"runtime"},{"key":"POSTGRES_HOST_AUTH_METHOD","value":"scram-sha-256","scope":"runtime"},{"key":"PGDATA","value":"/var/lib/postgresql/data","scope":"runtime"},{"key":"DATABASE_URL","value":"postgres://{{draft.db_user}}:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/{{draft.db_name}}","scope":"runtime"},{"key":"PUBLIC_DATABASE_URL","value":"postgres://{{draft.db_user}}:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/{{draft.db_name}}","scope":"runtime"}]`,
	},
	{
		Name:        "Redis",
		Description: "In-memory key/value store. Runs from the official image.",
		Category:    "datastore",
		Icon:        "redis",
		Color:       "#FF4438",
		Mode:        "image",
		Image:       "redis:7-alpine",
		Port:        6379,
		// The official redis image reads no env var for auth, so REDIS_PASSWORD
		// alone is a no-op. CmdOverride enforces it via --requirepass; Draft
		// expands {{draft.*}} in CmdOverride at stamp time.
		CmdOverride: "redis-server --requirepass {{draft.password}} --appendonly yes",
		EnvVars:     `[{"key":"REDIS_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"REDIS_URL","value":"redis://:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/0","scope":"runtime"},{"key":"PUBLIC_REDIS_URL","value":"redis://:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/0","scope":"runtime"}]`,
	},
	{
		Name:        "MySQL",
		Description: "Relational database. Runs from the official image.",
		Category:    "datastore",
		Icon:        "mysql",
		Color:       "#4479A1",
		Mode:        "image",
		Image:       "mysql:8",
		Port:        3306,
		EnvVars: `[{"key":"MYSQL_ROOT_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"MYSQL_DATABASE","value":"{{draft.db_name}}","scope":"runtime"},{"key":"MYSQL_USER","value":"{{draft.db_user}}","scope":"runtime"},{"key":"MYSQL_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"MYSQL_ROOT_HOST","value":"%","scope":"runtime"},{"key":"MYSQL_LOG_CONSOLE","value":"true","scope":"runtime"},{"key":"DATABASE_URL","value":"mysql://{{draft.db_user}}:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/{{draft.db_name}}","scope":"runtime"},{"key":"PUBLIC_DATABASE_URL","value":"mysql://{{draft.db_user}}:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/{{draft.db_name}}","scope":"runtime"}]`,
	},
	{
		Name:        "MongoDB",
		Description: "Document database. Runs from the official image.",
		Category:    "datastore",
		Icon:        "mongodb",
		Color:       "#47A248",
		Mode:        "image",
		Image:       "mongo:7",
		Port:        27017,
		// Setting both MONGO_INITDB_ROOT_* vars makes the official entrypoint
		// create a root user in the `admin` database and auto-enable --auth, so
		// no CmdOverride is needed (unlike Redis). The connection URL uses
		// authSource=admin because that's where the root user lives.
		EnvVars: `[{"key":"MONGO_INITDB_ROOT_USERNAME","value":"{{draft.db_user}}","scope":"runtime"},{"key":"MONGO_INITDB_ROOT_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"MONGO_INITDB_DATABASE","value":"{{draft.db_name}}","scope":"runtime"},{"key":"DATABASE_URL","value":"mongodb://{{draft.db_user}}:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/{{draft.db_name}}?authSource=admin","scope":"runtime"},{"key":"PUBLIC_DATABASE_URL","value":"mongodb://{{draft.db_user}}:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/{{draft.db_name}}?authSource=admin","scope":"runtime"}]`,
	},
}
