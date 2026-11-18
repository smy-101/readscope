// M6 fact probe: modernc.org/sqlite 的 FTS5 中文搜索能力。
// 运行: go run ./.scratch/m6-epub/ftsprobe
// 问题背景: unicode61 把连续 CJK 当单 token,子串搜不中;
// trigram 支持 ≥3 字子串匹配并加速 LIKE,但 2 字中文词 MATCH 不了。
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func check(db *sql.DB, label, q string, args ...any) {
	rows, err := db.Query(q, args...)
	if err != nil {
		fmt.Printf("%-28s ERR  %v\n", label, err)
		return
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	fmt.Printf("%-28s hit=%d\n", label, n)
}

func main() {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer db.Close()

	var ver string
	_ = db.QueryRow("SELECT sqlite_version()").Scan(&ver)
	fmt.Println("sqlite_version:", ver)

	fmt.Println("--- unicode61(默认) ---")
	if _, err := db.Exec(`CREATE VIRTUAL TABLE u USING fts5(c)`); err != nil {
		fmt.Println("create u:", err)
		return
	}
	db.Exec(`INSERT INTO u(c) VALUES ('终端里的统一阅读器,支持中文全文搜索。English mixed in.')`)
	check(db, "u MATCH '阅读'", `SELECT rowid FROM u WHERE u MATCH '阅读'`)
	check(db, "u MATCH '搜索。English'", `SELECT rowid FROM u WHERE u MATCH '搜索。English'`)

	fmt.Println("--- trigram ---")
	if _, err := db.Exec(`CREATE VIRTUAL TABLE t USING fts5(c, tokenize='trigram')`); err != nil {
		fmt.Println("create t:", err)
		return
	}
	db.Exec(`INSERT INTO t(c) VALUES ('终端里的统一阅读器,支持中文全文搜索。English mixed in.')`)
	check(db, "t MATCH '统一阅读'(4字)", `SELECT rowid FROM t WHERE t MATCH '统一阅读'`)
	check(db, "t MATCH '阅读'(2字)", `SELECT rowid FROM t WHERE t MATCH '阅读'`)
	check(db, "t LIKE '%阅读%'(2字)", `SELECT rowid FROM t WHERE c LIKE '%阅读%'`)
	check(db, "t LIKE '%English%'", `SELECT rowid FROM t WHERE c LIKE '%English%'`)
	check(db, "t MATCH 'English mixed'", `SELECT rowid FROM t WHERE t MATCH 'English mixed'`)

	fmt.Println("--- 单字空格 hack(unicode61,预分词) ---")
	if _, err := db.Exec(`CREATE VIRTUAL TABLE h USING fts5(c)`); err != nil {
		fmt.Println("create h:", err)
		return
	}
	// 把 CJK 逐字加空格后入库;查询同样处理,2 字词用短语查询 "阅 读"
	db.Exec(`INSERT INTO h(c) VALUES ('终 端 里 的 统 一 阅 读 器 支 持 中 文 全 文 搜 索 English mixed')`)
	check(db, `h MATCH '"阅 读"'(短语2字)`, `SELECT rowid FROM h WHERE h MATCH '"阅 读"'`)
	check(db, `h MATCH '"全文 搜 索"'(4字)`, `SELECT rowid FROM h WHERE h MATCH '"全 文 搜 索"'`)
}
