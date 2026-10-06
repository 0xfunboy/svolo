package server

import (
	"context"
	"errors"
	"svolo.local/core/internal/ssh"
)

func (s *Server) bootstrapHost(ctx context.Context, id string, o ssh.BootstrapOptions) (any, error) {
	if !s.Vault.Unlocked() {
		return nil, errors.New("unlock the credential vault before provisioning an SSH host")
	}
	h, ok := s.Config.Host(id)
	if !ok {
		return nil, errors.New("unknown host")
	}
	if o.Mode == "" {
		o.Mode = "process"
	}
	if o.Platform.OS == "" {
		p, err := s.SSH.Detect(ctx, h)
		if err != nil {
			return nil, err
		}
		o.Platform = p
	}
	binaryPath, err := ssh.LocateArtifact(s.artifactsDir, o.Platform)
	if err != nil {
		return nil, err
	}
	result, err := s.SSH.Bootstrap(ctx, h, o, binaryPath, s.Vault.Put)
	if err != nil {
		return nil, err
	}
	h.TokenRef = result.CredentialRef
	h.TokenEnv = ""
	if err = s.Config.UpsertHost(h); err != nil {
		return nil, err
	}
	_, _ = s.Store.Append("", "host.installed", map[string]any{"id": id, "platform": result.Platform, "sha256": result.SHA256, "version": result.Installation.Info.Version})
	return result, nil
}
