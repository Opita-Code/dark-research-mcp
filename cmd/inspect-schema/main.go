package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	db := os.Getenv("DARK_DB")
	if db == "" {
		db = os.Getenv("LOCALAPPDATA") + `\dark-agents\dark.db`
	}
	conn, err := sql.Open("sqlite", db)
	if err != nil {
		fmt.Println("open error:", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("=== tables in %s ===\n", db)
	rows, err := conn.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		fmt.Println("query error:", err)
		os.Exit(1)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		tables = append(tables, name)
	}
	for _, t := range tables {
		fmt.Printf("  %s\n", t)
	}

	// Row counts grouped by table-family. Constitution system tables
	// (added in v2) and the schema_migrations bookkeeping are surfaced
	// explicitly so the agent / user can verify the DB is at the
	// expected version.
	fmt.Println("\n=== row counts ===")
	for _, t := range tables {
		if len(t) >= 5 && t[:5] == "vibe_" {
			var n int
			_ = conn.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n)
			fmt.Printf("  %s: %d\n", t, n)
		}
	}
	fmt.Println("\n--- constitution system (v2) ---")
	for _, t := range []string{"constitutions", "mods", "mod_loads"} {
		var n int
		_ = conn.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n)
		fmt.Printf("  %s: %d\n", t, n)
	}
	fmt.Println("\n--- sdd_evaluations (anti-refusal audit, v3) ---")
	var sddN int
	_ = conn.QueryRow("SELECT COUNT(*) FROM sdd_evaluations").Scan(&sddN)
	fmt.Printf("  sdd_evaluations: %d\n", sddN)
	var refusedN int
	_ = conn.QueryRow("SELECT COUNT(*) FROM sdd_evaluations WHERE refused_attempts > 0").Scan(&refusedN)
	fmt.Printf("  ...where refused_attempts > 0: %d\n", refusedN)
}