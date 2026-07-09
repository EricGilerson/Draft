package store

// This file holds the curated built-in service templates. Add new built-ins
// here; SeedBuiltins (in templates.go) inserts them on first run and is
// idempotent on Name, so additions land on the next Open of a fresh database.
//
// Dockerfiles are embedded so the later "create service" flow can write them
// into a service root without the user needing to author one. Datastore entries
// use Mode=="image" and carry an image name instead of a Dockerfile; the
// image-pull deploy path runs them straight from the registry.
//
// Each built-in also carries a Schema (see template_schema.go) that drives the
// create-service wizard and the Settings tab. The two canned schemas below keep
// the slice literal readable.

// buildTemplateSchema is the schema for the build-mode built-ins: everything
// optional, full 4-step wizard, no hidden Settings sections.
var buildTemplateSchema = mustEncodeSchema(TemplateSchema{
	ServiceRoot: SchemaOptional,
	Dockerfile:  SchemaOptional,
	WizardSteps: []WizardStep{
		{ID: "template", Title: "Template"},
		{ID: "identity", Title: "Name & options"},
		{ID: "source", Title: "Service root"},
		{ID: "review", Title: "Review"},
	},
})

// tcpWireDefaults stamps pure wire-protocol datastores onto TCP routing with a
// preferred host port matching the container service port. That makes
// postgres://…@*.draft.resolv.sh:5432 (etc.) work when the preferred port is free.
func tcpWireDefaults(hostPort string) string {
	return MustEncodeDefaultSettings(map[string]string{
		"route_protocol": "tcp",
		"host_port":      hostPort,
	})
}

// imageTemplateSchema is the schema for the image-mode datastore built-ins:
// service root and Dockerfile are irrelevant, the source/build Settings
// sections are hidden, and the wizard skips the source step. Volumes are
// intentionally NOT hidden — datastores are the services that most need
// persistent storage, so a Volumes wizard step and Settings section are shown
// and seeded from the template's Volumes defaults.
var imageTemplateSchema = mustEncodeSchema(TemplateSchema{
	ServiceRoot: SchemaHidden,
	Dockerfile:  SchemaHidden,
	HideSections: []string{
		SectionSource,
		SectionDockerfile,
		SectionBuildContext,
		SectionBuildConfig,
		SectionRuntimeCommand,
	},
	Volumes: &VolumeCapability{Show: true, Editable: true},
	WizardSteps: []WizardStep{
		{ID: "template", Title: "Template"},
		{ID: "identity", Title: "Name & options"},
		{ID: "volumes", Title: "Volumes"},
		{ID: "review", Title: "Review"},
	},
})

// prebuiltImageSchema is the schema for the "Prebuilt Image" built-in: a
// generic image-mode service where the user supplies the image ref and port.
// Source/Dockerfile/build sections are hidden (there is nothing to build), but
// runtime command, volumes, restart, healthcheck, resources, lifecycle,
// security, and labels remain available in Settings since a user's own image
// may need any of them. The wizard is minimal — pick template, name + image +
// port, review — with no source or volumes step.
var prebuiltImageSchema = mustEncodeSchema(TemplateSchema{
	ServiceRoot: SchemaHidden,
	Dockerfile:  SchemaHidden,
	HideSections: []string{
		SectionSource,
		SectionDockerfile,
		SectionBuildContext,
		SectionBuildConfig,
	},
	WizardSteps: []WizardStep{
		{ID: "template", Title: "Template"},
		{ID: "identity", Title: "Name & options"},
		{ID: "review", Title: "Review"},
	},
})

func mustEncodeSchema(s TemplateSchema) string {
	out, err := EncodeTemplateSchema(s)
	if err != nil {
		panic(err)
	}
	return out
}

// Curated image tag lists for the datastore built-ins. The first entry is the
// template's default (it matches the tag in Image); the rest are common
// alternatives the user can pick from in the create wizard / Settings → Image
// without typing a full ref. Users can always type a custom ref too.
var (
	postgresImageTags = `["16-alpine","16","15-alpine","15","14-alpine","14","latest"]`
	redisImageTags    = `["7-alpine","7","6-alpine","6","latest"]`
	mysqlImageTags    = `["8","8.0","8-debian","latest"]`
	mongoImageTags    = `["7","7-jammy","6","6-jammy","latest"]`
)

// Default volume mounts for the datastore built-ins. Each is a Draft-managed
// named volume (type:"volume", empty source => Draft mints a deterministic
// name from the node identity at deploy time). This is what makes a freshly
// created database persist across redeploys — without it, every stop/redeploy
// would lose all data. Targets match each official image's documented data
// directory so the entrypoint writes into the mounted volume.
var (
	postgresVolumes    = `[{"type":"volume","containerPath":"/var/lib/postgresql/data"}]`
	mysqlVolumes       = `[{"type":"volume","containerPath":"/var/lib/mysql"}]`
	mongoVolumes       = `[{"type":"volume","containerPath":"/data/db"},{"type":"volume","containerPath":"/data/configdb"}]`
	redisVolumes       = `[{"type":"volume","containerPath":"/data"}]`
	minioVolumes       = `[{"type":"volume","containerPath":"/data"}]`
	rabbitmqVolumes    = `[{"type":"volume","containerPath":"/var/lib/rabbitmq"}]`
	meilisearchVolumes = `[{"type":"volume","containerPath":"/meili_data"}]`
	clickhouseVolumes  = `[{"type":"volume","containerPath":"/var/lib/clickhouse"}]`
)

var (
	minioImageTags       = `["latest"]`
	rabbitmqImageTags    = `["3-management-alpine","3-management","3.13-management-alpine","latest"]`
	meilisearchImageTags = `["v1.10","v1.9","latest"]`
	memcachedImageTags   = `["1.6-alpine","1.6","latest"]`
	clickhouseImageTags  = `["24-alpine","24","23-alpine","latest"]`
	mailpitImageTags     = `["latest"]`
	adminerImageTags     = `["latest","4-standalone","4"]`
)

var builtinTemplates = []ServiceTemplate{
	{
		Name:        "Next.js",
		Description: "Production Next.js: multi-stage build served by `next start`.",
		Category:    "web",
		Icon:        "nextdotjs",
		Color:       "#000000",
		Mode:        "build",
		Port:        3000,
		Schema:      buildTemplateSchema,
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
		Schema:      buildTemplateSchema,
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
		Name:        "Go",
		Description: "Compiled Go service built as a static binary.",
		Category:    "language",
		Icon:        "go",
		Color:       "#00ADD8",
		Mode:        "build",
		Port:        8080,
		Schema:      buildTemplateSchema,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/bin ./...

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/bin ./bin
EXPOSE 8080
CMD ["./bin"]
`,
		EnvVars: `[{"key":"PORT","value":"8080","scope":"runtime"}]`,
	},
	{
		Name:        "Rust",
		Description: "Compiled Rust service built with cargo in release mode.",
		Category:    "language",
		Icon:        "rust",
		Color:       "#CE422B",
		Mode:        "build",
		Port:        8080,
		Schema:      buildTemplateSchema,
		// The binary name "app" is a placeholder for the crate's [package].name
		// in Cargo.toml — update both the build output path and CMD to match.
		Dockerfile: `# syntax=docker/dockerfile:1
FROM rust:1.82-alpine AS builder
WORKDIR /app
RUN apk add --no-cache musl-dev
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/target/release/app ./app
EXPOSE 8080
CMD ["./app"]
`,
		EnvVars: `[{"key":"PORT","value":"8080","scope":"runtime"}]`,
	},
	{
		Name:        "Deno",
		Description: "Deno service run directly from source — no separate build step.",
		Category:    "language",
		Icon:        "deno",
		Color:       "#000000",
		Mode:        "build",
		Port:        8000,
		Schema:      buildTemplateSchema,
		// "main.ts" is a placeholder entry point — update it to match your project.
		Dockerfile: `# syntax=docker/dockerfile:1
FROM denoland/deno:alpine
WORKDIR /app
COPY . .
RUN deno cache main.ts
EXPOSE 8000
CMD ["deno", "run", "--allow-net", "--allow-env", "main.ts"]
`,
		EnvVars: `[{"key":"PORT","value":"8000","scope":"runtime"}]`,
	},
	{
		Name:        "FastAPI / Uvicorn",
		Description: "ASGI Python service served by Gunicorn with Uvicorn workers.",
		Category:    "web",
		Icon:        "fastapi",
		Color:       "#009688",
		Mode:        "build",
		Port:        8000,
		Schema:      buildTemplateSchema,
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
		Schema:      buildTemplateSchema,
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
		Schema:      buildTemplateSchema,
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
		Name:        "Express",
		Description: "Express.js service run via `npm start`.",
		Category:    "web",
		Icon:        "express",
		Color:       "#000000",
		Mode:        "build",
		Port:        3000,
		Schema:      buildTemplateSchema,
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
		EnvVars: `[{"key":"NODE_ENV","value":"production","scope":"runtime"},{"key":"PORT","value":"3000","scope":"runtime"}]`,
	},
	{
		Name:        "Django",
		Description: "Django service served by Gunicorn. Wire `python manage.py migrate` as a post-start lifecycle hook rather than baking it into the container command.",
		Category:    "web",
		Icon:        "django",
		Color:       "#092E20",
		Mode:        "build",
		Port:        8000,
		Schema:      buildTemplateSchema,
		// "config.wsgi" matches the layout `django-admin startproject config`
		// produces — update it if your project module is named differently.
		Dockerfile: `# syntax=docker/dockerfile:1
FROM python:3.12-slim
WORKDIR /app
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["gunicorn", "config.wsgi:application", "-w", "2", "-b", "0.0.0.0:8000"]
`,
		EnvVars: `[{"key":"DJANGO_SETTINGS_MODULE","value":"config.settings","scope":"runtime"},{"key":"SECRET_KEY","value":"{{draft.password}}","scope":"runtime"},{"key":"DJANGO_ALLOWED_HOSTS","value":"*","scope":"runtime"},{"key":"PYTHONUNBUFFERED","value":"1","scope":"runtime"}]`,
	},
	{
		Name:        "Ruby on Rails",
		Description: "Rails service served by Puma. Wire `rails db:migrate` as a post-start lifecycle hook rather than baking it into the container command.",
		Category:    "web",
		Icon:        "rubyonrails",
		Color:       "#CC0000",
		Mode:        "build",
		Port:        3000,
		Schema:      buildTemplateSchema,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
WORKDIR /app
RUN apt-get update -qq && apt-get install -y --no-install-recommends build-essential git libpq-dev && rm -rf /var/lib/apt/lists/*
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
ENV RAILS_ENV=production
EXPOSE 3000
CMD ["bundle", "exec", "puma", "-b", "tcp://0.0.0.0:3000"]
`,
		// SECRET_KEY_BASE is tripled since {{draft.password}} alone (24 chars) is
		// shorter than Rails' recommended minimum secret length.
		EnvVars: `[{"key":"RAILS_ENV","value":"production","scope":"runtime"},{"key":"SECRET_KEY_BASE","value":"{{draft.password}}{{draft.password}}{{draft.password}}","scope":"runtime"},{"key":"RAILS_LOG_TO_STDOUT","value":"1","scope":"runtime"},{"key":"RAILS_SERVE_STATIC_FILES","value":"true","scope":"runtime"}]`,
	},
	{
		Name:        "Phoenix",
		Description: "Elixir/Phoenix service built as an OTP release.",
		Category:    "web",
		Icon:        "elixir",
		Color:       "#4B275F",
		Mode:        "build",
		Port:        4000,
		Schema:      buildTemplateSchema,
		// The release name "app" is a placeholder for your app's OTP release
		// name (set in mix.exs) — update the copy path and CMD to match.
		Dockerfile: `# syntax=docker/dockerfile:1
FROM elixir:1.17-alpine AS builder
WORKDIR /app
RUN apk add --no-cache build-base git
ENV MIX_ENV=prod
RUN mix local.hex --force && mix local.rebar --force
COPY mix.exs mix.lock ./
RUN mix deps.get --only prod
COPY . .
RUN mix assets.deploy || true
RUN mix release

FROM alpine:3.20
RUN apk add --no-cache openssl ncurses-libs libstdc++
WORKDIR /app
COPY --from=builder /app/_build/prod/rel/app ./
ENV HOME=/app
EXPOSE 4000
CMD ["bin/app", "start"]
`,
		// SECRET_KEY_BASE is tripled since Phoenix requires at least 64 bytes and
		// {{draft.password}} alone (24 chars) is shorter than that.
		EnvVars: `[{"key":"MIX_ENV","value":"prod","scope":"runtime"},{"key":"SECRET_KEY_BASE","value":"{{draft.password}}{{draft.password}}{{draft.password}}","scope":"runtime"},{"key":"PHX_HOST","value":"{{draft.internal_hostname}}","scope":"runtime"},{"key":"PHX_SERVER","value":"true","scope":"runtime"},{"key":"PORT","value":"4000","scope":"runtime"}]`,
	},
	{
		Name:        ".NET",
		Description: "ASP.NET Core service published in Release configuration.",
		Category:    "web",
		Icon:        "dotnet",
		Color:       "#512BD4",
		Mode:        "build",
		Port:        8080,
		Schema:      buildTemplateSchema,
		// "app.dll" is a placeholder for your project's assembly name — update
		// the ENTRYPOINT to match the .dll that `dotnet publish` produces.
		Dockerfile: `# syntax=docker/dockerfile:1
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS builder
WORKDIR /app
COPY *.csproj ./
RUN dotnet restore
COPY . .
RUN dotnet publish -c Release -o /out

FROM mcr.microsoft.com/dotnet/aspnet:8.0
WORKDIR /app
COPY --from=builder /out ./
ENV ASPNETCORE_URLS=http://+:8080
ENV ASPNETCORE_ENVIRONMENT=Production
EXPOSE 8080
ENTRYPOINT ["dotnet", "app.dll"]
`,
		EnvVars: `[{"key":"ASPNETCORE_ENVIRONMENT","value":"Production","scope":"runtime"},{"key":"ASPNETCORE_URLS","value":"http://+:8080","scope":"runtime"}]`,
	},
	{
		Name:        "Spring Boot",
		Description: "Spring Boot service built with Maven, packaged as a runnable jar.",
		Category:    "web",
		Icon:        "springboot",
		Color:       "#6DB33F",
		Mode:        "build",
		Port:        8080,
		Schema:      buildTemplateSchema,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM maven:3.9-eclipse-temurin-21 AS builder
WORKDIR /app
COPY pom.xml ./
RUN mvn -B dependency:go-offline
COPY src ./src
RUN mvn -B package -DskipTests

FROM eclipse-temurin:21-jre-alpine
WORKDIR /app
COPY --from=builder /app/target/*.jar ./app.jar
ENV JAVA_OPTS="-Xmx512m"
EXPOSE 8080
CMD ["sh", "-c", "java $JAVA_OPTS -jar app.jar"]
`,
		EnvVars: `[{"key":"SPRING_PROFILES_ACTIVE","value":"production","scope":"runtime"},{"key":"JAVA_OPTS","value":"-Xmx512m","scope":"runtime"}]`,
	},
	{
		Name:        "Static Site",
		Description: "Static assets served by nginx — no build step. Drop pre-built HTML/CSS/JS into the service root.",
		Category:    "web",
		Icon:        "nginx",
		Color:       "#009639",
		Mode:        "build",
		Port:        8080,
		Schema:      buildTemplateSchema,
		Dockerfile: `# syntax=docker/dockerfile:1
FROM nginx:1.27-alpine
RUN sed -i 's/listen[[:space:]]*80;/listen 8080;/' /etc/nginx/conf.d/default.conf
COPY . /usr/share/nginx/html
EXPOSE 8080
CMD ["nginx", "-g", "daemon off;"]
`,
	},
	{
		Name:            "PostgreSQL",
		Description:     "Relational database. Runs from the official image.",
		Category:        "datastore",
		Icon:            "postgresql",
		Color:           "#4169E1",
		Mode:            "image",
		Image:           "postgres:16-alpine",
		Port:            5432,
		Schema:          imageTemplateSchema,
		ImageTags:       postgresImageTags,
		Volumes:         postgresVolumes,
		DefaultSettings: tcpWireDefaults("5432"),
		// DB user/name use the official image's standard defaults (postgres/postgres)
		// rather than being derived from the project, so credentials read the way a
		// freshly-installed Postgres would. The password stays per-node derived so
		// two Postgres instances in different projects never share a secret.
		EnvVars: `[{"key":"POSTGRES_USER","value":"postgres","scope":"runtime"},{"key":"POSTGRES_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"POSTGRES_DB","value":"postgres","scope":"runtime"},{"key":"POSTGRES_HOST_AUTH_METHOD","value":"scram-sha-256","scope":"runtime"},{"key":"PGDATA","value":"/var/lib/postgresql/data","scope":"runtime"},{"key":"DATABASE_URL","value":"postgres://postgres:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/postgres","scope":"runtime"},{"key":"PUBLIC_DATABASE_URL","value":"postgres://postgres:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/postgres","scope":"runtime"}]`,
	},
	{
		Name:            "Redis",
		Description:     "In-memory key/value store. Runs from the official image.",
		Category:        "datastore",
		Icon:            "redis",
		Color:           "#FF4438",
		Mode:            "image",
		Image:           "redis:7-alpine",
		Port:            6379,
		Schema:          imageTemplateSchema,
		ImageTags:       redisImageTags,
		Volumes:         redisVolumes,
		DefaultSettings: tcpWireDefaults("6379"),
		// The official redis image reads no env var for auth, so REDIS_PASSWORD
		// alone is a no-op. CmdOverride enforces it via --requirepass; Draft
		// expands {{draft.*}} in CmdOverride at stamp time.
		CmdOverride: "redis-server --requirepass {{draft.password}} --appendonly yes",
		EnvVars:     `[{"key":"REDIS_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"REDIS_URL","value":"redis://:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/0","scope":"runtime"},{"key":"PUBLIC_REDIS_URL","value":"redis://:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/0","scope":"runtime"}]`,
	},
	{
		Name:            "MySQL",
		Description:     "Relational database. Runs from the official image.",
		Category:        "datastore",
		Icon:            "mysql",
		Color:           "#4479A1",
		Mode:            "image",
		Image:           "mysql:8",
		Port:            3306,
		Schema:          imageTemplateSchema,
		ImageTags:       mysqlImageTags,
		Volumes:         mysqlVolumes,
		DefaultSettings: tcpWireDefaults("3306"),
		// Standard defaults: root is the admin (password derived per node), and
		// an `mysql` app user is created with access to the `appdb` database. Both
		// are fixed conventions independent of the project name.
		EnvVars: `[{"key":"MYSQL_ROOT_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"MYSQL_DATABASE","value":"appdb","scope":"runtime"},{"key":"MYSQL_USER","value":"mysql","scope":"runtime"},{"key":"MYSQL_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"MYSQL_ROOT_HOST","value":"%","scope":"runtime"},{"key":"MYSQL_LOG_CONSOLE","value":"true","scope":"runtime"},{"key":"DATABASE_URL","value":"mysql://mysql:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/appdb","scope":"runtime"},{"key":"PUBLIC_DATABASE_URL","value":"mysql://mysql:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/appdb","scope":"runtime"}]`,
	},
	{
		Name:            "MongoDB",
		Description:     "Document database. Runs from the official image.",
		Category:        "datastore",
		Icon:            "mongodb",
		Color:           "#47A248",
		Mode:            "image",
		Image:           "mongo:7",
		Port:            27017,
		Schema:          imageTemplateSchema,
		ImageTags:       mongoImageTags,
		Volumes:         mongoVolumes,
		DefaultSettings: tcpWireDefaults("27017"),
		// Setting both MONGO_INITDB_ROOT_* vars makes the official entrypoint
		// create a root user in the `admin` database and auto-enable --auth, so
		// no CmdOverride is needed (unlike Redis). The connection URL uses
		// authSource=admin because that's where the root user lives. The root
		// username is the conventional `root` (independent of the project), and
		// MONGO_INITDB_DATABASE seeds an `appdb` for the app to use.
		EnvVars: `[{"key":"MONGO_INITDB_ROOT_USERNAME","value":"root","scope":"runtime"},{"key":"MONGO_INITDB_ROOT_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"MONGO_INITDB_DATABASE","value":"appdb","scope":"runtime"},{"key":"DATABASE_URL","value":"mongodb://root:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/appdb?authSource=admin","scope":"runtime"},{"key":"PUBLIC_DATABASE_URL","value":"mongodb://root:{{draft.password}}@{{draft.public_hostname}}:{{draft.service_port}}/appdb?authSource=admin","scope":"runtime"}]`,
	},
	{
		Name:        "MinIO",
		Description: "S3-compatible object storage with a web console. Runs from the official image.",
		Category:    "datastore",
		Icon:        "minio",
		Color:       "#C72E49",
		Mode:        "image",
		Image:       "minio/minio:latest",
		Port:        9001,
		Schema:      imageTemplateSchema,
		ImageTags:   minioImageTags,
		Volumes:     minioVolumes,
		// MinIO's entrypoint needs explicit args: the data dir and the console
		// bind address. service_port routes the console (9001, browser-facing);
		// the S3 API stays at the image's fixed 9000 and is reached by sibling
		// containers directly over the Docker network, same as any other
		// container-to-container port that Draft doesn't need to proxy to the host.
		CmdOverride: `server /data --console-address ":9001"`,
		EnvVars:     `[{"key":"MINIO_ROOT_USER","value":"minioadmin","scope":"runtime"},{"key":"MINIO_ROOT_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"S3_ENDPOINT","value":"http://{{draft.internal_hostname}}:9000","scope":"runtime"},{"key":"AWS_ACCESS_KEY_ID","value":"minioadmin","scope":"runtime"},{"key":"AWS_SECRET_ACCESS_KEY","value":"{{draft.password}}","scope":"runtime"},{"key":"AWS_REGION","value":"us-east-1","scope":"runtime"}]`,
	},
	{
		Name:        "RabbitMQ",
		Description: "Message broker with the management UI. Runs from the official image.",
		Category:    "datastore",
		Icon:        "rabbitmq",
		Color:       "#FF6600",
		Mode:        "image",
		Image:       "rabbitmq:3-management-alpine",
		Port:        15672,
		Schema:      imageTemplateSchema,
		ImageTags:   rabbitmqImageTags,
		Volumes:     rabbitmqVolumes,
		// service_port routes the management UI (15672); AMQP stays at the
		// image's fixed 5672 and is reached by sibling containers directly.
		EnvVars: `[{"key":"RABBITMQ_DEFAULT_USER","value":"rabbitmq","scope":"runtime"},{"key":"RABBITMQ_DEFAULT_PASS","value":"{{draft.password}}","scope":"runtime"},{"key":"AMQP_URL","value":"amqp://rabbitmq:{{draft.password}}@{{draft.internal_hostname}}:5672/","scope":"runtime"}]`,
	},
	{
		Name:        "Meilisearch",
		Description: "Lightweight search engine with a built-in dashboard. Runs from the official image.",
		Category:    "datastore",
		Icon:        "meilisearch",
		Color:       "#FF5CAA",
		Mode:        "image",
		Image:       "getmeili/meilisearch:v1.10",
		Port:        7700,
		Schema:      imageTemplateSchema,
		ImageTags:   meilisearchImageTags,
		Volumes:     meilisearchVolumes,
		EnvVars:     `[{"key":"MEILI_MASTER_KEY","value":"{{draft.password}}","scope":"runtime"},{"key":"MEILI_ENV","value":"development","scope":"runtime"},{"key":"MEILI_URL","value":"http://{{draft.internal_hostname}}:7700","scope":"runtime"}]`,
	},
	{
		Name:            "Memcached",
		Description:     "In-memory cache with no persistence or built-in auth. Runs from the official image.",
		Category:        "datastore",
		Icon:            "memcached",
		Mode:            "image",
		Image:           "memcached:1.6-alpine",
		Port:            11211,
		Schema:          imageTemplateSchema,
		ImageTags:       memcachedImageTags,
		DefaultSettings: tcpWireDefaults("11211"),
		EnvVars:         `[{"key":"MEMCACHED_URL","value":"{{draft.internal_hostname}}:11211","scope":"runtime"}]`,
	},
	{
		Name:        "ClickHouse",
		Description: "Columnar analytics database with an HTTP query API and Play UI. Runs from the official image.",
		Category:    "datastore",
		Icon:        "clickhouse",
		Color:       "#FFCC01",
		Mode:        "image",
		Image:       "clickhouse/clickhouse-server:24-alpine",
		Port:        8123,
		Schema:      imageTemplateSchema,
		ImageTags:   clickhouseImageTags,
		Volumes:     clickhouseVolumes,
		// ClickHouse's HTTP interface (8123) serves both the query API and the
		// built-in Play UI at /play, so a single routed port covers both uses.
		EnvVars: `[{"key":"CLICKHOUSE_USER","value":"default","scope":"runtime"},{"key":"CLICKHOUSE_PASSWORD","value":"{{draft.password}}","scope":"runtime"},{"key":"CLICKHOUSE_DB","value":"appdb","scope":"runtime"},{"key":"DATABASE_URL","value":"http://default:{{draft.password}}@{{draft.internal_hostname}}:8123/appdb","scope":"runtime"}]`,
	},
	{
		Name:        "Mailpit",
		Description: "Local SMTP catcher with a web UI to inspect outgoing mail. Point any service's mailer at this node's internal hostname on port 1025. Runs from the official image.",
		Category:    "tooling",
		Icon:        "mailpit",
		Mode:        "image",
		Image:       "axllent/mailpit:latest",
		Port:        8025,
		Schema:      imageTemplateSchema,
		ImageTags:   mailpitImageTags,
	},
	{
		Name:        "Adminer",
		Description: "Single-page database admin UI for Postgres/MySQL. After deploying, log in with a sibling service's internal hostname and credentials.",
		Category:    "tooling",
		Icon:        "adminer",
		Mode:        "image",
		Image:       "adminer:latest",
		Port:        8080,
		Schema:      imageTemplateSchema,
		ImageTags:   adminerImageTags,
	},
	{
		Name:        "Prebuilt Image",
		Description: "Run any container image you already have — locally built or from a registry. No Dockerfile or source needed.",
		Category:    "image",
		Icon:        "docker",
		Color:       "#2496ED",
		Mode:        "image",
		// Image and Port are intentionally empty: the user supplies both in the
		// create wizard (required) or later in Settings. No curated tag list —
		// the wizard renders a free-text image field for this template.
		Schema: prebuiltImageSchema,
	},
}
