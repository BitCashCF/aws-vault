package vault

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/99designs/keyring"
	"github.com/aws/aws-sdk-go/service/sts"
)

var sessionKeyPattern = regexp.MustCompile(`session \(\d+\)$`)

func IsSessionKey(s string) bool {
	return sessionKeyPattern.MatchString(s)
}

type KeyringSessions struct {
	Keyring keyring.Keyring
	Config  *Config
}

func NewKeyringSessions(k keyring.Keyring, cfg *Config) (*KeyringSessions, error) {
	return &KeyringSessions{
		Keyring: k,
		Config:  cfg,
	}, nil
}

func (s *KeyringSessions) key(profileName string, duration time.Duration) string {
	source, _ := s.Config.SourceProfile(profileName)

	hasher := md5.New()
	hasher.Write([]byte(duration.String()))

	sourceHash, err := source.Hash()
	if err != nil {
		log.Panicf("Error hashing profile %q: %v", profileName, err)
	}

	hasher.Write(sourceHash)

	return fmt.Sprintf("%s session (%x)", source, hex.EncodeToString(hasher.Sum(nil))[0:10])
}

func (s *KeyringSessions) Retrieve(profile string, duration time.Duration) (session sts.Credentials, err error) {
	item, err := s.Keyring.Get(s.key(profile, duration))
	if err != nil {
		return session, err
	}

	if err = json.Unmarshal(item.Data, &session); err != nil {
		return session, err
	}

	if session.Expiration.Before(time.Now()) {
		return session, errors.New("Session is expired")
	}

	return
}

func (s *KeyringSessions) Store(profile string, session sts.Credentials, duration time.Duration) error {
	bytes, err := json.Marshal(session)
	if err != nil {
		return err
	}

	log.Printf("Writing session for %s to keyring", profile)
	s.Keyring.Set(keyring.Item{
		Key:         s.key(profile, duration),
		Label:       "aws-vault session for " + profile,
		Description: "aws-vault session for " + profile,
		Data:        bytes,
		TrustSelf:   false,
	})

	return nil
}

func (s *KeyringSessions) Delete(profile string) (n int, err error) {
	keys, err := s.Keyring.Keys()
	if err != nil {
		return n, err
	}

	source, _ := s.Config.SourceProfile(profile)

	for _, k := range keys {
		if strings.HasPrefix(k, fmt.Sprintf("%s session", source.Name)) {
			if err = s.Keyring.Remove(k); err != nil {
				return n, err
			}
			n++
		}
	}

	return
}
