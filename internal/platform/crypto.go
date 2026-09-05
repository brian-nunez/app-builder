package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brian-nunez/app-builder/sdk/go/plugin"
)

type Cipher struct{ aead cipher.AEAD }

func NewCipher(key string) (*Cipher, error) {
	b, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(b) != 32 {
		return nil, errors.New("security.encryption_key must be a base64 encoded 32-byte key")
	}
	block, err := aes.NewCipher(b)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	return &Cipher{a}, err
}
func (c *Cipher) Seal(v any, scope string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	n := make([]byte, c.aead.NonceSize())
	if _, err = rand.Read(n); err != nil {
		return nil, err
	}
	return c.aead.Seal(n, n, b, []byte(scope)), nil
}
func (c *Cipher) Open(b []byte, scope string, v any) error {
	n := c.aead.NonceSize()
	if len(b) < n {
		return errors.New("invalid encrypted payload")
	}
	p, err := c.aead.Open(nil, b[:n], b[n:], []byte(scope))
	if err != nil {
		return err
	}
	return json.Unmarshal(p, v)
}

func (s *Store) encodeGraph(g plugin.Graph, workflowID string, revision int) (plugin.Graph, error) {
	result := g
	result.Nodes = append([]plugin.Node{}, g.Nodes...)
	for i, n := range result.Nodes {
		sealed, err := s.Cipher.Seal(n.Config, fmt.Sprintf("%s:%d:%s:config", workflowID, revision, n.ID))
		if err != nil {
			return result, err
		}
		result.Nodes[i].Config = map[string]any{"__workflow_sealed_config_v1": base64.StdEncoding.EncodeToString(sealed)}
	}
	return result, nil
}
func (s *Store) decodeGraph(raw []byte, workflowID string, revision int) (plugin.Graph, error) {
	var g plugin.Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		return g, err
	}
	for i, n := range g.Nodes {
		encoded, ok := n.Config["__workflow_sealed_config_v1"].(string)
		if !ok {
			continue
		}
		sealed, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return g, err
		}
		var config map[string]any
		if err = s.Cipher.Open(sealed, fmt.Sprintf("%s:%d:%s:config", workflowID, revision, n.ID), &config); err != nil {
			return g, err
		}
		g.Nodes[i].Config = config
	}
	return g, nil
}
