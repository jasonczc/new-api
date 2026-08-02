package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFishVoiceTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err, "open test db")
	require.NoError(t, db.AutoMigrate(&FishVoice{}), "migrate FishVoice")
	original := DB
	DB = db
	t.Cleanup(func() {
		db.Exec("DELETE FROM fish_voices")
		DB = original
	})
}

func TestFishVoiceOwnershipIsolation(t *testing.T) {
	setupFishVoiceTestDB(t)

	owner := &FishVoice{UserId: 1, ChannelId: 7, UpstreamId: "voice-a", Title: "Alice"}
	require.NoError(t, owner.Insert())

	// the owner can read it back, with the creating channel preserved
	got, err := GetFishVoiceByUpstreamId(1, "voice-a")
	require.NoError(t, err, "owner lookup")
	assert.Equal(t, 7, got.ChannelId, "mutations must return to the creating account")
	assert.NotZero(t, got.CreatedTime, "CreatedTime should be stamped on insert")

	// another user must not see it, and must not be able to tell it exists
	_, err = GetFishVoiceByUpstreamId(2, "voice-a")
	assert.ErrorIs(t, err, ErrFishVoiceNotFound, "cross-user lookup")
	_, err = GetFishVoiceByUpstreamId(2, "does-not-exist")
	assert.ErrorIs(t, err, ErrFishVoiceNotFound, "missing voice")

	// deleting as a non-owner must not remove the row
	require.NoError(t, DeleteFishVoice(2, "voice-a"), "cross-user delete")
	_, err = GetFishVoiceByUpstreamId(1, "voice-a")
	require.NoError(t, err, "voice was removed by a non-owner delete")

	// the owner can delete it
	require.NoError(t, DeleteFishVoice(1, "voice-a"), "owner delete")
	_, err = GetFishVoiceByUpstreamId(1, "voice-a")
	assert.ErrorIs(t, err, ErrFishVoiceNotFound, "voice still present after owner delete")
}

func TestFishVoiceListIsScopedToUser(t *testing.T) {
	setupFishVoiceTestDB(t)

	for _, v := range []*FishVoice{
		{UserId: 1, ChannelId: 7, UpstreamId: "a", Title: "mine-1"},
		{UserId: 1, ChannelId: 8, UpstreamId: "b", Title: "mine-2"},
		{UserId: 2, ChannelId: 7, UpstreamId: "c", Title: "theirs"},
	} {
		require.NoError(t, v.Insert(), "insert %s", v.UpstreamId)
	}

	voices, err := GetFishVoicesByUser(1)
	require.NoError(t, err, "list")
	require.Len(t, voices, 2, "the shared upstream account holds 3, user 1 owns 2")
	for _, voice := range voices {
		assert.Equal(t, 1, voice.UserId, "listing leaked a voice owned by user %d", voice.UserId)
	}
}

func TestFishVoiceUpdateMeta(t *testing.T) {
	setupFishVoiceTestDB(t)

	voice := &FishVoice{UserId: 1, ChannelId: 7, UpstreamId: "voice-x", Title: "old", Visibility: "private", State: "trained"}
	require.NoError(t, voice.Insert())

	require.NoError(t, voice.UpdateMeta("new", "public", ""))
	got, err := GetFishVoiceByUpstreamId(1, "voice-x")
	require.NoError(t, err, "lookup")
	assert.Equal(t, "new", got.Title)
	assert.Equal(t, "public", got.Visibility)
	// empty fields must not blank out existing values
	assert.Equal(t, "trained", got.State, "State must be preserved")
}
