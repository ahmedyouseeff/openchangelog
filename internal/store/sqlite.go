package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jonashiltl/openchangelog/apitypes"
	"github.com/jonashiltl/openchangelog/internal/errs"
	_ "github.com/mattn/go-sqlite3"

	"github.com/guregu/null/v5"
)

func (cl changelog) toExported(source changelogSource) Changelog {
	c := Changelog{
		WorkspaceID:   WorkspaceID(cl.WorkspaceID),
		ID:            ChangelogID(cl.ID),
		Subdomain:     Subdomain(cl.Subdomain),
		Domain:        Domain(cl.Domain),
		Title:         cl.Title,
		Subtitle:      cl.Subtitle,
		LogoSrc:       cl.LogoSrc,
		LogoLink:      cl.LogoLink,
		LogoAlt:       cl.LogoAlt,
		LogoHeight:    cl.LogoHeight,
		LogoWidth:     cl.LogoWidth,
		ColorScheme:   cl.ColorScheme,
		HidePoweredBy: cl.HidePoweredBy == 1,
		Protected:     cl.Protected == 1,
		Analytics:     cl.Analytics == 1,
		Searchable:    cl.Searchable == 1,
		PasswordHash:  cl.PasswordHash.V(),
		CreatedAt:     time.Unix(cl.CreatedAt, 0),
		GHSource:      null.NewValue(GHSource{}, false),
	}

	if !source.ID.IsNull() && source.ID.IsValid() && !source.WorkspaceID.IsNull() && source.WorkspaceID.IsValid() {
		c.GHSource = null.NewValue(GHSource{
			ID:             GHSourceID(source.ID.V()),
			WorkspaceID:    WorkspaceID(source.WorkspaceID.V()),
			Owner:          source.Owner.V(),
			Repo:           source.Repo.V(),
			Path:           source.Path.V(),
			InstallationID: source.InstallationID.Int64,
		}, true)
	}
	return c
}

func (gh ghSource) toExported() GHSource {
	return GHSource{
		ID:             GHSourceID(gh.ID),
		WorkspaceID:    WorkspaceID(gh.WorkspaceID),
		Owner:          gh.Owner,
		Repo:           gh.Repo,
		Path:           gh.Path,
		InstallationID: gh.InstallationID,
	}
}

func NewSQLiteStore(conn string) (Store, error) {
	// Extract database file path and ensure directory exists
	dbPath := extractDBPath(conn)
	if dbPath != "" {
		if err := ensureDBDirectory(dbPath); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", conn)
	if err != nil {
		return nil, err
	}

	// Force SQLite to create the database file if it doesn't exist
	_, err = db.Exec("SELECT 1")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	q := New(db)

	return &sqlite{
		q:  q,
		db: db,
	}, nil
}

// extractDBPath extracts the file path from a SQLite connection string
// Handles formats like: file:path, file:///path, file:./path
func extractDBPath(conn string) string {
	// Remove query parameters
	parts := strings.Split(conn, "?")
	path := parts[0]

	// Handle file: prefix
	if strings.HasPrefix(path, "file:") {
		path = strings.TrimPrefix(path, "file:")
		// Handle file:/// format
		if strings.HasPrefix(path, "///") {
			path = strings.TrimPrefix(path, "///")
		} else if strings.HasPrefix(path, "//") {
			// file://host/path format (not common for SQLite)
			return ""
		}
	}

	return path
}

// ensureDBDirectory creates the directory for the database file if it doesn't exist
func ensureDBDirectory(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir == "" || dir == "." {
		return nil // No directory needed (current directory)
	}
	return os.MkdirAll(dir, 0755)
}

// runMigrations executes database migrations in order
func runMigrations(db *sql.DB) error {
	migrationsDir := "migrations"
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		// If migrations directory doesn't exist, log a warning but don't fail
		// This allows the app to work if migrations are run separately
		slog.Warn("migrations directory not found, skipping automatic migrations", "dir", migrationsDir)
		return nil
	}

	var migrationFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrationFiles = append(migrationFiles, entry.Name())
		}
	}

	if len(migrationFiles) == 0 {
		slog.Warn("no migration files found")
		return nil
	}

	sort.Strings(migrationFiles)

	for _, filename := range migrationFiles {
		content, err := os.ReadFile(filepath.Join(migrationsDir, filename))
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", filename, err)
		}

		// Extract SQL between goose Up markers
		sqlContent := string(content)
		lines := strings.Split(sqlContent, "\n")
		var upSQL strings.Builder
		inUpSection := false

		for _, line := range lines {
			if strings.Contains(line, "-- +goose Up") {
				inUpSection = true
				continue
			}
			if strings.Contains(line, "-- +goose Down") {
				break
			}
			if inUpSection && !strings.Contains(line, "-- +goose StatementBegin") && !strings.Contains(line, "-- +goose StatementEnd") {
				upSQL.WriteString(line + "\n")
			}
		}

		if upSQL.Len() > 0 {
			_, err = db.Exec(upSQL.String())
			if err != nil {
				return fmt.Errorf("failed to execute migration %s: %w", filename, err)
			}
			slog.Debug("executed migration", "file", filename)
		}
	}

	slog.Info("database migrations completed successfully", "count", len(migrationFiles))
	return nil
}

type sqlite struct {
	q  *Queries
	db *sql.DB
}

func (s *sqlite) CreateChangelog(ctx context.Context, cl Changelog) (Changelog, error) {
	c, err := s.q.createChangelog(ctx, createChangelogParams{
		ID:            cl.ID.String(),
		WorkspaceID:   cl.WorkspaceID.String(),
		Subdomain:     cl.Subdomain.String(),
		Domain:        cl.Domain.NullString(),
		Title:         cl.Title,
		Subtitle:      cl.Subtitle,
		LogoSrc:       cl.LogoSrc,
		LogoLink:      cl.LogoLink,
		LogoAlt:       cl.LogoAlt,
		LogoHeight:    cl.LogoHeight,
		LogoWidth:     cl.LogoWidth,
		ColorScheme:   cl.ColorScheme,
		HidePoweredBy: boolToInt(cl.HidePoweredBy),
		Protected:     boolToInt(cl.Protected),
		Analytics:     boolToInt(cl.Analytics),
		Searchable:    boolToInt(cl.Searchable),
		PasswordHash:  apitypes.NewString(cl.PasswordHash),
	})
	if err != nil {
		return Changelog{}, formatUnqueConstraint(err)
	}

	// TODO get source
	return c.toExported(changelogSource{}), nil
}

var errNoChangelog = errs.NewError(errs.ErrNotFound, errors.New("changelog not found"))

func (s *sqlite) GetChangelog(ctx context.Context, wID WorkspaceID, cID ChangelogID) (Changelog, error) {
	cl, err := s.q.getChangelog(ctx, getChangelogParams{
		WorkspaceID: wID.String(),
		ID:          cID.String(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Changelog{}, errNoChangelog
		}
		return Changelog{}, err
	}

	return cl.changelog.toExported(cl.ChangelogSource), nil
}

func (s *sqlite) GetChangelogByDomainOrSubdomain(ctx context.Context, domain Domain, subdomain Subdomain) (Changelog, error) {
	cl, err := s.q.getChangelogByDomainOrSubdomain(ctx, getChangelogByDomainOrSubdomainParams{
		Domain:    domain.NullString(),
		Subdomain: subdomain.String(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Changelog{}, errNoChangelog
		}
		return Changelog{}, err
	}

	return cl.changelog.toExported(cl.ChangelogSource), nil
}

func (s *sqlite) ListChangelogs(ctx context.Context, wID WorkspaceID) ([]Changelog, error) {
	cls, err := s.q.listChangelogs(ctx, wID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return make([]Changelog, 0), nil
		}
		return nil, err
	}

	res := make([]Changelog, len(cls))
	for i, cl := range cls {
		res[i] = cl.changelog.toExported(cl.ChangelogSource)
	}
	return res, nil
}

// dereferences b to it's int representation
func saveDerefToInt(b *bool) int64 {
	if b != nil && *b {
		return 1
	}
	return 0
}

// Returns 1 if b is true, otherwise 2
func boolToInt(b bool) int64 {
	var i int64
	if b {
		i = 1
	}
	return i
}

func (s *sqlite) UpdateChangelog(ctx context.Context, wID WorkspaceID, cID ChangelogID, args UpdateChangelogArgs) (Changelog, error) {
	// does not update string fields if they are zero value
	_, err := s.q.updateChangelog(ctx, updateChangelogParams{
		ID:          cID.String(),
		WorkspaceID: wID.String(),
		Subdomain:   args.Subdomain,
		HidePoweredBy: sql.NullInt64{ // update if HidePoweredBy != nil
			Int64: saveDerefToInt(args.HidePoweredBy),
			Valid: args.HidePoweredBy != nil,
		},
		ColorScheme:    args.ColorScheme,
		SetColorScheme: int(args.ColorScheme) != 0,
		Title:          args.Title,
		SetTitle:       !args.Title.IsZero(),
		Subtitle:       args.Subtitle,
		SetSubtitle:    !args.Subtitle.IsZero(),
		Domain:         args.Domain.NullString(),
		SetDomain:      !args.Domain.NullString().IsZero(),
		LogoSrc:        args.LogoSrc,
		SetLogoSrc:     !args.LogoSrc.IsZero(),
		LogoLink:       args.LogoLink,
		SetLogoLink:    !args.LogoLink.IsZero(),
		LogoAlt:        args.LogoAlt,
		SetLogoAlt:     !args.LogoAlt.IsZero(),
		LogoHeight:     args.LogoHeight,
		SetLogoHeight:  !args.LogoHeight.IsZero(),
		LogoWidth:      args.LogoWidth,
		SetLogoWidth:   !args.LogoWidth.IsZero(),
		Protected: sql.NullInt64{ // update if args.Protected is defined
			Int64: saveDerefToInt(args.Protected),
			Valid: args.Protected != nil,
		},
		Analytics: sql.NullInt64{ // update if args.Analytics is defined
			Int64: saveDerefToInt(args.Analytics),
			Valid: args.Analytics != nil,
		},
		Searchable: sql.NullInt64{ // update if args.Searchable is defined
			Int64: saveDerefToInt(args.Searchable),
			Valid: args.Searchable != nil,
		},
		PasswordHash:    args.PasswordHash,
		SetPasswordHash: !args.PasswordHash.IsZero(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Changelog{}, errNoChangelog
		}
		return Changelog{}, formatUnqueConstraint(err)
	}
	return s.GetChangelog(ctx, wID, cID)
}

// If err is a unique constraint error, return humanized error message.
// Otherwise return err
func formatUnqueConstraint(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed: changelogs.subdomain") {
		return errs.NewBadRequest(errors.New("subdomain already taken, please try again with a different one"))
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed: changelogs.domain") {
		return errs.NewBadRequest(errors.New("domain already taken, please try again with a different one"))
	}
	return err
}

func (s *sqlite) DeleteChangelog(ctx context.Context, wID WorkspaceID, cID ChangelogID) error {
	return s.q.deleteChangelog(ctx, deleteChangelogParams{
		WorkspaceID: wID.String(),
		ID:          cID.String(),
	})
}

func (s *sqlite) SetChangelogGHSource(ctx context.Context, wID WorkspaceID, cID ChangelogID, ghID GHSourceID) error {
	return s.q.setChangelogSource(ctx, setChangelogSourceParams{
		SourceID:    apitypes.NewString(ghID.String()),
		WorkspaceID: wID.String(),
		ID:          cID.String(),
	})
}

func (s *sqlite) DeleteChangelogSource(ctx context.Context, wID WorkspaceID, cID ChangelogID) error {
	return s.q.deleteChangelogSource(ctx, deleteChangelogSourceParams{
		WorkspaceID: wID.String(),
		ID:          cID.String(),
	})
}

func (s *sqlite) SaveWorkspace(ctx context.Context, ws Workspace) (Workspace, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)

	c, err := q.saveWorkspace(ctx, saveWorkspaceParams{
		ID:   ws.ID.String(),
		Name: ws.Name,
	})
	if err != nil {
		return Workspace{}, err
	}

	if ws.Token != "" {
		err := q.createToken(ctx, createTokenParams{
			Key:         ws.Token.String(),
			WorkspaceID: ws.ID.String(),
		})
		if err != nil {
			return Workspace{}, err
		}
	}

	err = tx.Commit()
	if err != nil {
		return Workspace{}, err
	}

	return Workspace{
		ID:    WorkspaceID(c.ID),
		Name:  c.Name,
		Token: ws.Token,
	}, nil
}

func (s *sqlite) GetWorkspace(ctx context.Context, wID WorkspaceID) (Workspace, error) {
	row, err := s.q.getWorkspace(ctx, wID.String())
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{
		ID:    WorkspaceID(row.workspace.ID),
		Name:  row.workspace.Name,
		Token: Token(row.token.Key),
	}, nil
}

func (s *sqlite) GetWorkspaceIDByToken(ctx context.Context, token string) (WorkspaceID, error) {
	row, err := s.q.getToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errs.NewError(errs.ErrUnauthorized, errors.New("invalid bearer token"))
		}
		return "", err
	}
	return WorkspaceID(row.WorkspaceID), nil
}

func (s *sqlite) DeleteWorkspace(ctx context.Context, wID WorkspaceID) error {
	return s.q.deleteWorkspace(ctx, wID.String())
}

func (s *sqlite) CreateGHSource(ctx context.Context, gh GHSource) (GHSource, error) {
	row, err := s.q.createGHSource(ctx, createGHSourceParams{
		WorkspaceID:    gh.WorkspaceID.String(),
		ID:             gh.ID.String(),
		Owner:          gh.Owner,
		Repo:           gh.Repo,
		Path:           gh.Path,
		InstallationID: gh.InstallationID,
	})
	if err != nil {
		return GHSource{}, err
	}
	return row.toExported(), nil
}

func (s *sqlite) DeleteGHSource(ctx context.Context, wID WorkspaceID, ghID GHSourceID) error {
	return s.q.deleteGHSource(ctx, deleteGHSourceParams{
		WorkspaceID: wID.String(),
		ID:          ghID.String(),
	})
}

func (s *sqlite) GetGHSource(ctx context.Context, wID WorkspaceID, ghID GHSourceID) (GHSource, error) {
	row, err := s.q.getGHSource(ctx, getGHSourceParams{
		WorkspaceID: wID.String(),
		ID:          ghID.String(),
	})
	if err != nil {
		return GHSource{}, err
	}
	return row.toExported(), nil
}

func (s *sqlite) ListGHSources(ctx context.Context, wID WorkspaceID) ([]GHSource, error) {
	rows, err := s.q.listGHSources(ctx, wID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return make([]GHSource, 0), nil
		}
		return nil, err
	}

	sources := make([]GHSource, len(rows))
	for i, row := range rows {
		sources[i] = row.toExported()
	}
	return sources, nil
}

func (s *sqlite) ListWorkspacesChangelogCount(ctx context.Context) ([]WorkspaceChangelogCount, error) {
	rows, err := s.q.listWorkspacesChangelogCount(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return make([]WorkspaceChangelogCount, 0), nil
		}
		return nil, err
	}
	res := make([]WorkspaceChangelogCount, len(rows))
	for i, row := range rows {
		res[i] = WorkspaceChangelogCount{
			Workspace: Workspace{
				ID:   WorkspaceID(row.workspace.ID),
				Name: row.workspace.Name,
			},
			ChangelogCount: row.ChangelogCount,
		}
	}
	return res, nil
}
