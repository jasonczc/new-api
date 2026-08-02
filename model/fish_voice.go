package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// FishVoice records ownership of a Fish Audio voice model created through this
// gateway.
//
// Fish Audio channels share one upstream API key across all gateway users, so
// the upstream account cannot distinguish who created which voice. Without this
// table any user could list, mutate or delete every other user's voices. Each
// row also pins the channel the voice lives on, so later reads and mutations go
// back to the same upstream account that created it.
type FishVoice struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	UserId      int    `json:"user_id" gorm:"index"`
	ChannelId   int    `json:"channel_id" gorm:"index"`
	UpstreamId  string `json:"upstream_id" gorm:"type:varchar(64);uniqueIndex"`
	Title       string `json:"title" gorm:"type:varchar(255)"`
	State       string `json:"state" gorm:"type:varchar(32)"`
	Visibility  string `json:"visibility" gorm:"type:varchar(32)"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

var ErrFishVoiceNotFound = errors.New("voice model not found")

func (v *FishVoice) Insert() error {
	if v.CreatedTime == 0 {
		v.CreatedTime = time.Now().Unix()
	}
	return DB.Create(v).Error
}

// GetFishVoiceByUpstreamId looks a voice up scoped to its owner. Callers must
// pass the requesting user's id; a voice owned by somebody else is reported as
// not found so the endpoint cannot be used to probe for existence.
func GetFishVoiceByUpstreamId(userId int, upstreamId string) (*FishVoice, error) {
	if upstreamId == "" {
		return nil, ErrFishVoiceNotFound
	}
	var voice FishVoice
	err := DB.Where("user_id = ? AND upstream_id = ?", userId, upstreamId).First(&voice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrFishVoiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &voice, nil
}

// IsFishVoiceOwnedByOther reports whether a voice created through this gateway
// belongs to a user other than the one asking for it. Synthesis requests carry
// a voice id straight to the shared upstream key, which would otherwise let any
// caller speak in another user's cloned voice.
//
// Ids this gateway has no record of are deliberately treated as not owned by
// anyone: Fish Audio's public voice catalogue and voices created outside the
// gateway are legitimate to use and are not ours to gate.
func IsFishVoiceOwnedByOther(userId int, upstreamId string) (bool, error) {
	if upstreamId == "" {
		return false, nil
	}
	var count int64
	err := DB.Model(&FishVoice{}).
		Where("upstream_id = ? AND user_id <> ?", upstreamId, userId).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func GetFishVoicesByUser(userId int) ([]*FishVoice, error) {
	var voices []*FishVoice
	err := DB.Where("user_id = ?", userId).Order("id desc").Find(&voices).Error
	return voices, err
}

func (v *FishVoice) UpdateMeta(title, visibility, state string) error {
	updates := map[string]any{}
	if title != "" {
		updates["title"] = title
	}
	if visibility != "" {
		updates["visibility"] = visibility
	}
	if state != "" {
		updates["state"] = state
	}
	if len(updates) == 0 {
		return nil
	}
	return DB.Model(v).Updates(updates).Error
}

func DeleteFishVoice(userId int, upstreamId string) error {
	return DB.Where("user_id = ? AND upstream_id = ?", userId, upstreamId).Delete(&FishVoice{}).Error
}
