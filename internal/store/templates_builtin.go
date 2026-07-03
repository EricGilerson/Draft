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
		Description: "React framework with dev server, hot reload, and SSR.",
		Category:    "web",
		Icon:        "nextdotjs",
		Color:       "#000000",
		Mode:        "build",
		Port:        3000,
		Dockerfile: `FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
EXPOSE 3000
CMD ["npm", "run", "dev"]
`,
		EnvVars: `[{"key":"NODE_ENV","value":"development","scope":"runtime"},{"key":"PORT","value":"3000","scope":"runtime"}]`,
	},
	{
		Name:        "Node.js",
		Description: "Generic Node.js service from package.json.",
		Category:    "language",
		Icon:        "nodedotjs",
		Color:       "#5FA04E",
		Mode:        "build",
		Port:        3000,
		Dockerfile: `FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
`,
		EnvVars: `[{"key":"NODE_ENV","value":"development","scope":"runtime"}]`,
	},
	{
		Name:        "FastAPI / Uvicorn",
		Description: "ASGI Python service served by Uvicorn.",
		Category:    "web",
		Icon:        "fastapi",
		Color:       "#009688",
		Mode:        "build",
		Port:        8000,
		Dockerfile: `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
`,
		EnvVars: `[{"key":"PYTHONUNBUFFERED","value":"1","scope":"runtime"},{"key":"UVICORN_HOST","value":"0.0.0.0","scope":"runtime"},{"key":"UVICORN_PORT","value":"8000","scope":"runtime"}]`,
	},
	{
		Name:        "Flask",
		Description: "Lightweight WSGI Python service.",
		Category:    "web",
		Icon:        "flask",
		Color:       "#000000",
		Mode:        "build",
		Port:        5000,
		Dockerfile: `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 5000
CMD ["flask", "run", "--host", "0.0.0.0", "--port", "5000"]
`,
		EnvVars: `[{"key":"FLASK_APP","value":"app.py","scope":"runtime"},{"key":"PYTHONUNBUFFERED","value":"1","scope":"runtime"}]`,
	},
	{
		Name:        "Vite",
		Description: "Vite dev server for React/Vue/Svelte SPAs.",
		Category:    "web",
		Icon:        "vite",
		Color:       "#646CFF",
		Mode:        "build",
		Port:        5173,
		Dockerfile: `FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
`,
		EnvVars: `[{"key":"NODE_ENV","value":"development","scope":"runtime"}]`,
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
		EnvVars:     `[{"key":"POSTGRES_USER","value":"draft","scope":"runtime"},{"key":"POSTGRES_PASSWORD","value":"draft","scope":"runtime"},{"key":"POSTGRES_DB","value":"draft","scope":"runtime"}]`,
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
		EnvVars:     `[{"key":"MYSQL_ROOT_PASSWORD","value":"draft","scope":"runtime"},{"key":"MYSQL_DATABASE","value":"draft","scope":"runtime"}]`,
	},
}
