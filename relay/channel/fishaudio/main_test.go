package fishaudio

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain sets gin's test mode once for the whole package. Doing it here rather
// than per-test keeps the write to gin's package globals out of the parallel
// tests, which would otherwise race under `go test -race`.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
