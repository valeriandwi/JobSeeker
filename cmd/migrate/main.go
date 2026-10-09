// Command migrate applies the plain-SQL migrations embedded in the binary.
//
// Usage: migrate [up|down|status|version] [-- env]
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	"github.com/pressly/goose/v3"

	"github.com/valeriandwipurnomo/jobseeker/internal/platform/config"
	"github.com/valeriandwipurnomo/jobseeker/migrations"
)

func main() {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping database (%s): %v", cfg.RedactedDatabaseURL(), err)
	}

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("dialect: %v", err)
	}

	switch command {
	case "up":
		if err := goose.UpContext(ctx, db, "."); err != nil {
			log.Fatalf("migrate up: %v", err)
		}
	case "down":
		if err := goose.DownContext(ctx, db, "."); err != nil {
			log.Fatalf("migrate down: %v", err)
		}
	case "status":
		if err := goose.StatusContext(ctx, db, "."); err != nil {
			log.Fatalf("migrate status: %v", err)
		}
		return
	case "version":
		v, err := goose.GetDBVersionContext(ctx, db)
		if err != nil {
			log.Fatalf("migrate version: %v", err)
		}
		fmt.Printf("%d\n", v)
		return
	default:
		log.Fatalf("unknown command %q (want up|down|status|version)", command)
	}

	v, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		log.Fatalf("reading version: %v", err)
	}
	fmt.Printf("migrations %s complete; current version: %d\n", command, v)
}
