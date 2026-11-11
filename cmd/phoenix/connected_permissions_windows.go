package main

import "encoding/json"

const connectedPermissionsKey = "connected_tool_permissions"

type connectedPermission struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
}

func connectedPermissions() []connectedPermission {
	list := []connectedPermission{}
	if lib, err := openLibrary(); err == nil {
		_ = json.Unmarshal([]byte(lib.Setting(connectedPermissionsKey)), &list)
	}
	return list
}

func connectedToolAllowed(server, tool string) bool {
	for _, p := range connectedPermissions() {
		if p.Server == server && p.Tool == tool {
			return true
		}
	}
	return false
}

func saveConnectedTool(server, tool string, allow bool) error {
	lib, err := openLibrary()
	if err != nil {
		return err
	}
	list := connectedPermissions()
	filtered := make([]connectedPermission, 0, len(list)+1)
	for _, p := range list {
		if p.Server != server || p.Tool != tool {
			filtered = append(filtered, p)
		}
	}
	if allow && server != "" && tool != "" {
		filtered = append(filtered, connectedPermission{server, tool})
	}
	b, err := json.Marshal(filtered)
	if err != nil {
		return err
	}
	return lib.SetSetting(connectedPermissionsKey, string(b))
}

func revokeConnectedServer(server string) error {
	for _, p := range connectedPermissions() {
		if p.Server == server {
			if err := saveConnectedTool(p.Server, p.Tool, false); err != nil {
				return err
			}
		}
	}
	return nil
}
