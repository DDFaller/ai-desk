package deck

import (
	"encoding/json"
	"os/exec"
)

func (s *StateDB) tmuxSocket(inst *InstanceRecord) (string, error) {
	var metadata struct {
		Socket string `json:"tmux_socket_name"`
	}
	_ = json.Unmarshal([]byte(inst.ToolData), &metadata)
	if metadata.Socket != "" {
		return metadata.Socket, nil
	}
	var hasSocket int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('instances') WHERE name='tmux_socket_name'`).Scan(&hasSocket); err != nil {
		return "", err
	}
	if hasSocket != 0 {
		var name string
		if err := s.db.QueryRow(`SELECT COALESCE(tmux_socket_name,'') FROM instances WHERE id=?`, inst.ID).Scan(&name); err != nil {
			return "", err
		}
		if name != "" {
			return name, nil
		}
	}
	return "default", nil
}

func tmuxOnSocket(socket string, args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", socket}, args...)...)
}
