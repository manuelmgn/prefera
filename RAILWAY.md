# Deploying Prefera to Railway

This guide explains how to deploy Prefera to [Railway.app](https://railway.app).

## Quick Start

### 1. Connect your GitHub repository
- Go to [railway.app](https://railway.app)
- Click "New Project" → "Deploy from GitHub"
- Select your `prefera` repository
- Railway will automatically detect the Dockerfile and deploy

### 2. Configure Environment Variables
In the Railway dashboard, add these environment variables:

```
DB_PATH=/data/listas.db
DOMAINS_PATH=/app/config/link_domains.txt
TMPL_PATH=/app/templates
STATIC_PATH=/app/static
```

**Note:** The `PORT` variable is automatically set by Railway.

### 3. Set Up Persistent Storage (Optional)
By default, Railway uses ephemeral storage, which means your SQLite database will be deleted when the service restarts.

To add persistent storage:
1. In the Railway dashboard, click "Add a Volume"
2. Set Mount Path to `/data`
3. This will preserve your database between deployments

### 4. Deploy
- Click "Deploy" button
- Railway will build your Docker image and start the service
- Once deployed, you'll get a public URL (something like `https://prefera-production.up.railway.app`)

## How It Works

- **Dockerfile**: Railway automatically builds your Go binary in a multi-stage Docker build
- **Port**: Railway assigns a dynamic port via the `PORT` environment variable (main.go automatically reads this)
- **Database**: Stored in `/data/listas.db` (inside the volume if you added one)

## Troubleshooting

### Build fails
Check the Railway build logs. Make sure:
- `go.mod` and `go.sum` are in the repository
- All imports in `.go` files use the `prefera` module name

### Database not persisting
You need to add a Volume in Railway (see step 3 above).

### Port conflicts
Railway automatically assigns the correct port. The app reads the `PORT` env var.

## Redeploying

Any push to your main branch will automatically trigger a new deployment via Railway's GitHub integration.

## Regional Deployment

Railway allows you to choose deployment regions. You can change this in your project settings.

## Custom Domain

After deployment, you can:
1. Go to your Railway project settings
2. Under "Domains", add your custom domain
3. Configure DNS records according to Railway's instructions
