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

	fmt.Println("\n=== row counts in vibe_* tables ===")
	for _, t := range tables {
		if len(t) >= 5 && t[:5] == "vibe_" {
			var n int
			_ = conn.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n)
			fmt.Printf("  %s: %d\n", t, n)
		}
	}
}