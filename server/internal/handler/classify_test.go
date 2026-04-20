// Package handler (white-box) — tests for unexported classifySQL and stripSQLComments.
package handler

import (
	"testing"
)

// ── classifySQL ───────────────────────────────────────────────────────────────

func TestClassifySQL_SELECT(t *testing.T) {
	if got := classifySQL("SELECT * FROM t"); got != "read" {
		t.Errorf("classifySQL SELECT = %q; want read", got)
	}
}

func TestClassifySQL_SELECT_Whitespace(t *testing.T) {
	if got := classifySQL("   SELECT 1   "); got != "read" {
		t.Errorf("classifySQL(whitespace SELECT) = %q; want read", got)
	}
}

func TestClassifySQL_SELECT_CaseInsensitive(t *testing.T) {
	if got := classifySQL("select * from t"); got != "read" {
		t.Errorf("classifySQL(lowercase select) = %q; want read", got)
	}
}

func TestClassifySQL_WITH(t *testing.T) {
	sql := "WITH cte AS (SELECT 1 AS n) SELECT * FROM cte"
	if got := classifySQL(sql); got != "read" {
		t.Errorf("classifySQL WITH = %q; want read", got)
	}
}

func TestClassifySQL_VALUES(t *testing.T) {
	if got := classifySQL("VALUES (1, 2, 3)"); got != "read" {
		t.Errorf("classifySQL VALUES = %q; want read", got)
	}
}

func TestClassifySQL_EXPLAIN(t *testing.T) {
	if got := classifySQL("EXPLAIN SELECT * FROM t"); got != "read" {
		t.Errorf("classifySQL EXPLAIN = %q; want read", got)
	}
}

func TestClassifySQL_Pragma_Read(t *testing.T) {
	if got := classifySQL("PRAGMA journal_mode"); got != "read" {
		t.Errorf("classifySQL PRAGMA read = %q; want read", got)
	}
}

func TestClassifySQL_INSERT(t *testing.T) {
	if got := classifySQL("INSERT INTO t (a) VALUES (1)"); got != "write" {
		t.Errorf("classifySQL INSERT = %q; want write", got)
	}
}

func TestClassifySQL_UPDATE(t *testing.T) {
	if got := classifySQL("UPDATE t SET a = 1 WHERE id = 1"); got != "write" {
		t.Errorf("classifySQL UPDATE = %q; want write", got)
	}
}

func TestClassifySQL_DELETE(t *testing.T) {
	if got := classifySQL("DELETE FROM t WHERE id = 1"); got != "write" {
		t.Errorf("classifySQL DELETE = %q; want write", got)
	}
}

func TestClassifySQL_DROP(t *testing.T) {
	if got := classifySQL("DROP TABLE t"); got != "write" {
		t.Errorf("classifySQL DROP = %q; want write", got)
	}
}

func TestClassifySQL_ALTER(t *testing.T) {
	if got := classifySQL("ALTER TABLE t ADD COLUMN x TEXT"); got != "write" {
		t.Errorf("classifySQL ALTER = %q; want write", got)
	}
}

func TestClassifySQL_CREATE(t *testing.T) {
	if got := classifySQL("CREATE TABLE t (id INTEGER)"); got != "write" {
		t.Errorf("classifySQL CREATE = %q; want write", got)
	}
}

func TestClassifySQL_ATTACH(t *testing.T) {
	if got := classifySQL("ATTACH DATABASE '/tmp/x.db' AS x"); got != "write" {
		t.Errorf("classifySQL ATTACH = %q; want write", got)
	}
}

func TestClassifySQL_DETACH(t *testing.T) {
	if got := classifySQL("DETACH DATABASE x"); got != "write" {
		t.Errorf("classifySQL DETACH = %q; want write", got)
	}
}

func TestClassifySQL_REPLACE(t *testing.T) {
	if got := classifySQL("REPLACE INTO t (id, v) VALUES (1, 2)"); got != "write" {
		t.Errorf("classifySQL REPLACE = %q; want write", got)
	}
}

func TestClassifySQL_Pragma_Write(t *testing.T) {
	if got := classifySQL("PRAGMA journal_mode = WAL"); got != "write" {
		t.Errorf("classifySQL PRAGMA write = %q; want write", got)
	}
}

// ── multi-statement batches ───────────────────────────────────────────────────

func TestClassifySQL_MixedBatch_IsWrite(t *testing.T) {
	sql := "SELECT 1; DROP TABLE users"
	if got := classifySQL(sql); got != "write" {
		t.Errorf("classifySQL mixed batch = %q; want write", got)
	}
}

func TestClassifySQL_AllReads_IsRead(t *testing.T) {
	sql := "SELECT 1; SELECT 2; SELECT 3"
	if got := classifySQL(sql); got != "read" {
		t.Errorf("classifySQL all reads = %q; want read", got)
	}
}

func TestClassifySQL_BatchWithEmpty(t *testing.T) {
	// Trailing semicolon creates an empty statement — should still be read.
	sql := "SELECT 1;"
	if got := classifySQL(sql); got != "read" {
		t.Errorf("classifySQL trailing semicolon = %q; want read", got)
	}
}

func TestClassifySQL_EmptyBatch(t *testing.T) {
	if got := classifySQL(""); got != "unknown" {
		t.Errorf("classifySQL empty = %q; want unknown", got)
	}
}

func TestClassifySQL_WhitespaceOnly(t *testing.T) {
	if got := classifySQL("   "); got != "unknown" {
		t.Errorf("classifySQL whitespace-only = %q; want unknown", got)
	}
}

// ── stripSQLComments ──────────────────────────────────────────────────────────

func TestStripSQLComments_BlockComment(t *testing.T) {
	input := "/* this is a comment */ SELECT 1"
	got := stripSQLComments(input)
	if got == input {
		t.Error("block comment was not removed")
	}
	if got == "" {
		t.Error("stripSQLComments removed too much")
	}
}

func TestStripSQLComments_LineComment(t *testing.T) {
	input := "-- dangerous comment\nSELECT 1"
	got := stripSQLComments(input)
	if got == input {
		t.Error("line comment was not removed")
	}
}

func TestStripSQLComments_NoComments(t *testing.T) {
	input := "SELECT * FROM t WHERE id = 1"
	got := stripSQLComments(input)
	if got != input {
		t.Errorf("stripSQLComments modified comment-free SQL: %q", got)
	}
}

func TestStripSQLComments_MultilineBlock(t *testing.T) {
	input := "/* line one\n   line two */ SELECT 1"
	got := stripSQLComments(input)
	if got == input {
		t.Error("multiline block comment was not removed")
	}
}

func TestStripSQLComments_MultipleComments(t *testing.T) {
	input := "/* c1 */ SELECT /* c2 */ 1 -- trailing"
	got := stripSQLComments(input)
	if got == input {
		t.Error("multiple comments were not removed")
	}
}
