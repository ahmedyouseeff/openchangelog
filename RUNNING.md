# Running the Application

Simple steps to run the app before and after making template changes.

## Prerequisites

1. **Install Go** (version 1.23+)
2. **Install templ CLI**:
   ```bash
   go install github.com/a-h/templ/cmd/templ@latest
   ```
3. **Install Node.js** (for Tailwind CSS compilation)
4. **Install dependencies**:
   ```bash
   go mod download
   cd internal/handler/web && npm install && cd ../../..
   ```

## Running the App (Before Template Changes)

If you haven't made any template changes, simply run:

```bash
go run cmd/server.go -config openchangelog.yml
```

The app will be available at `http://localhost:6001` (or the address specified in your config).

## Running the App (After Template Changes)

After modifying `.templ` files or CSS files, follow these steps:

### Step 1: Generate Template Files

Generate Go code from `.templ` files:

```bash
templ generate
```

This will regenerate all `*_templ.go` files from the `.templ` source files.

### Step 2: Generate CSS Files (if you modified CSS)

If you modified CSS files in `internal/handler/web/css/`, regenerate the static CSS:

```bash
cd internal/handler/web
npm run gen:all
cd ../../..
```

Or generate individually:
- Base CSS: `npm run gen:base`
- Admin CSS: `npm run gen:admin`

### Step 3: Run the Application

```bash
go run cmd/server.go -config openchangelog.yml
```

## Quick Reference

### Template Files Location
- Template source: `components/*.templ`, `internal/handler/web/views/*.templ`
- Generated files: `*_templ.go` (auto-generated, don't edit manually)

### CSS Files Location
- Source: `internal/handler/web/css/*.css`
- Generated: `internal/handler/web/static/*.css`

### Watch Mode (Development)

For faster development, you can use watch mode:

**Watch templates** (in one terminal):
```bash
templ generate --watch
```

**Watch CSS** (in another terminal):
```bash
cd internal/handler/web
npm run watch:base  # or watch:admin
```

**Run the app** (in a third terminal):
```bash
go run cmd/server.go -config openchangelog.yml
```

## Troubleshooting

### Common Issues

- **Template errors**: Make sure `templ generate` runs successfully before starting the app
- **CSS not updating**: Regenerate CSS files with `npm run gen:all`
- **Import errors**: Run `go mod tidy` to fix dependency issues
- **Port already in use**: Change the `addr` in `openchangelog.yml` or stop the existing process

### Search Index Error

If you see an error like:
```
failed to open search index at ./data/search: cannot open index, metadata missing
```

This means the search index is corrupted. **Fix it by deleting the search index directory**:

```bash
rm -rf ./data/search
```

Then run the app again. It will automatically create a new search index on startup.

**Note**: The search index path is configured in `openchangelog.yml` under `search.disk.path`. Make sure to delete the correct directory if you've customized the path.

