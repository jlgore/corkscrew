package db

import (
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

// Every identifier the schema lifecycle quotes is built from the official provider catalog or
// a literal table name, so an unsafe one is a programmer error.
func TestQuoteSchemaIdentifierAssertsSafeName(t *testing.T) {
	if got := quoteSchemaIdentifier("aws_resources"); got != `"aws_resources"` {
		t.Fatalf("quoteSchemaIdentifier = %s", got)
	}
	asserttest.FailsWith(t, "safe schema identifier", func() {
		quoteSchemaIdentifier(`x"; drop table findings; --`)
	})
}
