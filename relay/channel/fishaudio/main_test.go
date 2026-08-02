package fishaudio

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testUserId is the caller every test speaks as, so a voice owned by any other
// id stands in for "somebody else's".
const testUserId = 1

// TestMain gives the package a database. Synthesis authorises every voice id it
// is asked to speak in against the ownership table, on both the HTTP and the
// websocket path, so those code paths need one to run at all.
func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open("file:fishaudio_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(&model.FishVoice{}); err != nil {
		panic(err)
	}
	model.DB = db
	os.Exit(m.Run())
}
