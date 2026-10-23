package deck

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/falluba/ai-desk/pkg/configpaths"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rotation"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AgentRecord is a durable identity; sessions inherit defaults at creation only.
type AgentRecord struct {
	ID, Name, Role, Team, Repository, Worktree, Home, Runtime, AccountProfile, SkillProfile string
	Archived                                                                                bool
	CreatedAt                                                                               int64
}

type CreateAgentOptions struct{ Name, Role, Team, Repository, Runtime, AccountProfile, SkillProfile string }

func (s *StateDB) ensureAgents() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS ai_desk_agents (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, role TEXT NOT NULL, team TEXT NOT NULL,
 repository TEXT NOT NULL, worktree TEXT NOT NULL, home TEXT NOT NULL,
 runtime TEXT NOT NULL, account_profile TEXT NOT NULL, skill_profile TEXT NOT NULL,
 archived INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL
 )`)
	return err
}

func AgentHomesPath() string {
	if p := os.Getenv("AI_DESK_AGENTS_DIR"); p != "" {
		return p
	}
	home, _ := configpaths.HomeDir()
	return filepath.Join(home, ".ai-desk", "agents")
}

func (s *StateDB) CreateAgent(opts CreateAgentOptions) (*AgentRecord, error) {
	if err := s.ensureAgents(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.Name) == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if opts.Runtime == "" {
		opts.Runtime = "codex"
	}
	tool, _, err := NormalizeHarnessName(opts.Runtime)
	if err != nil {
		return nil, err
	}
	if opts.SkillProfile == "" {
		opts.SkillProfile = "none"
	}
	skillCfg, err := harness.LoadSkillProfiles("")
	if err != nil {
		return nil, err
	}
	if _, err = skillCfg.ResolveSkills(opts.SkillProfile, ""); err != nil {
		return nil, err
	}
	profiles, err := rotation.LoadAccountProfiles("")
	if err != nil {
		return nil, err
	}
	if opts.AccountProfile == "" {
		opts.AccountProfile = profiles.ActiveProfile
	}
	if _, ok := profiles.Profiles[opts.AccountProfile]; !ok {
		return nil, fmt.Errorf("unknown account profile %q", opts.AccountProfile)
	}
	if opts.Team == "" {
		opts.Team = "my-agents"
	}
	repo, err := filepath.Abs(opts.Repository)
	if err != nil {
		return nil, err
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("repository must be an existing local Git checkout: %w", err)
	}
	repo = strings.TrimSpace(string(out))
	token := make([]byte, 12)
	if _, err = rand.Read(token); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(token)
	home := filepath.Join(AgentHomesPath(), id)
	worktree := filepath.Join(home, "worktree")
	if err = os.MkdirAll(filepath.Join(home, "memory"), 0700); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(home, "skills"), 0700); err != nil {
		return nil, err
	}
	out, err = exec.Command("git", "-C", repo, "worktree", "add", "-b", "ai-desk/"+id, worktree, "HEAD").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("create worktree (repository needs a commit): %w: %s", err, strings.TrimSpace(string(out)))
	}
	files := map[string]string{"AGENT.md": "# " + opts.Name + "\n\n" + opts.Role + "\n", "USER.md": "# User preferences\n", "MEMORY.md": "# Durable project notes\n"}
	for name, body := range files {
		if err = os.WriteFile(filepath.Join(home, "memory", name), []byte(body), 0600); err != nil {
			return nil, err
		}
	}
	if err = s.CreateGroup(opts.Team); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	_, err = s.db.Exec(`INSERT INTO ai_desk_agents (id,name,role,team,repository,worktree,home,runtime,account_profile,skill_profile,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, id, opts.Name, opts.Role, opts.Team, repo, worktree, home, tool, opts.AccountProfile, opts.SkillProfile, now)
	if err != nil {
		return nil, err
	}
	return &AgentRecord{ID: id, Name: opts.Name, Role: opts.Role, Team: opts.Team, Repository: repo, Worktree: worktree, Home: home, Runtime: tool, AccountProfile: opts.AccountProfile, SkillProfile: opts.SkillProfile, CreatedAt: now}, nil
}

func (s *StateDB) ListAgents(includeArchived bool) ([]AgentRecord, error) {
	if err := s.ensureAgents(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id,name,role,team,repository,worktree,home,runtime,account_profile,skill_profile,archived,created_at FROM ai_desk_agents WHERE archived=0 OR ? ORDER BY team,name,id`, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []AgentRecord
	for rows.Next() {
		var a AgentRecord
		if err = rows.Scan(&a.ID, &a.Name, &a.Role, &a.Team, &a.Repository, &a.Worktree, &a.Home, &a.Runtime, &a.AccountProfile, &a.SkillProfile, &a.Archived, &a.CreatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (s *StateDB) FindAgent(id string) (*AgentRecord, error) {
	agents, err := s.ListAgents(true)
	if err != nil {
		return nil, err
	}
	var match *AgentRecord
	for _, a := range agents {
		if a.ID == id {
			return &a, nil
		}
		if a.Name == id {
			if match != nil {
				return nil, fmt.Errorf("agent name is ambiguous; use its ID")
			}
			copy := a
			match = &copy
		}
	}
	if match == nil {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	return match, nil
}

func (s *StateDB) ArchiveAgent(id string, archived bool) error {
	a, err := s.FindAgent(id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE ai_desk_agents SET archived=? WHERE id=?`, archived, a.ID)
	return err
}

// RemoveAgentWorktree is explicit and never uses --force, including for dirty files.
func (s *StateDB) RemoveAgentWorktree(id string) error {
	a, err := s.FindAgent(id)
	if err != nil {
		return err
	}
	if !a.Archived {
		return fmt.Errorf("archive the agent before removing its worktree")
	}
	instances, err := s.LoadInstances()
	if err != nil {
		return err
	}
	for _, inst := range instances {
		cfg, _ := ReadSessionConfig(&inst)
		if cfg.AgentID == a.ID && inst.TmuxSession != "" {
			if exec.Command("tmux", "has-session", "-t", "="+inst.TmuxSession).Run() == nil {
				return fmt.Errorf("agent still has a live session")
			}
		}
	}
	out, err := exec.Command("git", "-C", a.Worktree, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("worktree has uncommitted or untracked files")
	}
	out, err = exec.Command("git", "-C", a.Repository, "worktree", "remove", a.Worktree).CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove worktree: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// AdoptSession associates an existing session without moving its working directory.
func (s *StateDB) AdoptSession(agentID, sessionID string) error {
	a, err := s.FindAgent(agentID)
	if err != nil {
		return err
	}
	if a.Archived {
		return fmt.Errorf("agent is archived")
	}
	inst, err := s.FindInstance(sessionID)
	if err != nil {
		return err
	}
	cfg, err := ReadSessionConfig(inst)
	if err != nil {
		return err
	}
	cfg.AgentID = a.ID
	return s.SaveSessionConfig(inst, cfg)
}

// BindAgentMemory writes generated bridges only inside the isolated runtime home.
func (s *StateDB) BindAgentMemory(v *harness.Virtualizer, inst *InstanceRecord) error {
	cfg, err := ReadSessionConfig(inst)
	if err != nil || cfg.AgentID == "" {
		return err
	}
	a, err := s.FindAgent(cfg.AgentID)
	if err != nil {
		return err
	}
	text := "\n<!-- ai-desk-agent-memory -->\nAgent memory directory: " + filepath.Join(a.Home, "memory") + "\nRecord durable preferences and lessons there; consolidate notes. Never store secrets or transient logs. Preserve project instructions. Skills require manual attachment.\n"
	for _, name := range []string{"AGENT.md", "USER.md", "MEMORY.md"} {
		data, err := os.ReadFile(filepath.Join(a.Home, "memory", name))
		if err != nil {
			return err
		}
		runes := []rune(string(data))
		if len(runes) > 2400 {
			runes = runes[:2400]
		}
		text += "\n## " + name + "\n" + string(runes) + "\n"
	}
	sandbox := SessionVirtualizer(v, inst).SandboxPath(inst.Tool, inst.Account)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		path := filepath.Join(sandbox, name)
		existing, _ := os.ReadFile(path)
		base := strings.SplitN(string(existing), "\n<!-- ai-desk-agent-memory -->", 2)[0]
		if err = os.MkdirAll(sandbox, 0700); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(sandbox, "memory-*.tmp")
		if err != nil {
			return err
		}
		_, writeErr := tmp.WriteString(base + text)
		closeErr := tmp.Close()
		if writeErr != nil {
			_ = os.Remove(tmp.Name())
			return writeErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp.Name())
			return closeErr
		}
		if err = os.Rename(tmp.Name(), path); err != nil {
			_ = os.Remove(tmp.Name())
			return err
		}
	}
	return nil
}

// AgentVirtualizer exposes local skills under agent-- names without installing them globally.
func (s *StateDB) AgentVirtualizer(v *harness.Virtualizer, agentID string) (*harness.Virtualizer, error) {
	if agentID == "" {
		return v, nil
	}
	a, err := s.FindAgent(agentID)
	if err != nil {
		return nil, err
	}
	catalog := filepath.Join(a.Home, "catalog")
	if err = os.MkdirAll(catalog, 0700); err != nil {
		return nil, err
	}
	sources := map[string]string{}
	names, err := harness.ListAllSkillNames(v.SkillsPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, name := range names {
		sources[name] = filepath.Join(v.SkillsPath(), name)
	}
	local, err := os.ReadDir(filepath.Join(a.Home, "skills"))
	if err != nil {
		return nil, err
	}
	for _, entry := range local {
		if entry.IsDir() {
			sources["agent--"+entry.Name()] = filepath.Join(a.Home, "skills", entry.Name())
		}
	}
	entries, err := os.ReadDir(catalog)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if _, ok := sources[entry.Name()]; !ok && entry.Type()&os.ModeSymlink != 0 {
			if err = os.Remove(filepath.Join(catalog, entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	for name, source := range sources {
		path := filepath.Join(catalog, name)
		target, err := os.Readlink(path)
		if err == nil && target == source {
			continue
		}
		if _, err = os.Lstat(path); err == nil {
			return nil, fmt.Errorf("catalog entry %q conflicts with skill source", name)
		}
		if err = os.Symlink(source, path); err != nil {
			return nil, err
		}
	}
	return v.WithSkillsPath(catalog), nil
}

// UpdateAgentDefaults affects future sessions, leaving existing selections untouched.
func (s *StateDB) UpdateAgentDefaults(id, runtime, accountProfile, skillProfile string) error {
	a, err := s.FindAgent(id)
	if err != nil {
		return err
	}
	if runtime != "" {
		normalized, _, err := NormalizeHarnessName(runtime)
		if err != nil {
			return err
		}
		a.Runtime = normalized
	}
	if accountProfile != "" {
		cfg, err := rotation.LoadAccountProfiles("")
		if err != nil {
			return err
		}
		if _, ok := cfg.Profiles[accountProfile]; !ok {
			return fmt.Errorf("unknown account profile %q", accountProfile)
		}
		a.AccountProfile = accountProfile
	}
	if skillProfile != "" {
		cfg, err := harness.LoadSkillProfiles("")
		if err != nil {
			return err
		}
		if _, err = cfg.ResolveSkills(skillProfile, ""); err != nil {
			return err
		}
		a.SkillProfile = skillProfile
	}
	_, err = s.db.Exec(`UPDATE ai_desk_agents SET runtime=?,account_profile=?,skill_profile=? WHERE id=?`, a.Runtime, a.AccountProfile, a.SkillProfile, a.ID)
	return err
}
