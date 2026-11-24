package gateway

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type MihomoConnectionMetadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	DestinationIP     string `json:"destinationIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationPort   string `json:"destinationPort"`
	Host              string `json:"host"`
	DNSMode           string `json:"dnsMode,omitempty"`
	ProcessPath       string `json:"processPath,omitempty"`
	RemoteDestination string `json:"remoteDestination,omitempty"`
}

type MihomoConnectionItem struct {
	ID          string                   `json:"id"`
	Metadata    MihomoConnectionMetadata `json:"metadata"`
	Upload      int64                    `json:"upload"`
	Download    int64                    `json:"download"`
	Start       string                   `json:"start"`
	Chains      []string                 `json:"chains"`
	Rule        string                   `json:"rule"`
	RulePayload string                   `json:"rulePayload"`
}

type MihomoConnectionsSnapshot struct {
	Running       bool                   `json:"running"`
	DownloadTotal int64                  `json:"download_total"`
	UploadTotal   int64                  `json:"upload_total"`
	Memory        int64                  `json:"memory,omitempty"`
	Connections   []MihomoConnectionItem `json:"connections"`
}

func (m *mihomoManager) GetConnections() (MihomoConnectionsSnapshot, error) {
	m.mu.Lock()
	running := m.running
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()

	out := MihomoConnectionsSnapshot{
		Running:     running,
		Connections: []MihomoConnectionItem{},
	}
	if !running || ctrlURL == "" {
		return out, nil
	}

	var payload struct {
		DownloadTotal int64                  `json:"downloadTotal"`
		UploadTotal   int64                  `json:"uploadTotal"`
		Memory        int64                  `json:"memory"`
		Connections   []MihomoConnectionItem `json:"connections"`
	}
	if err := m.doControllerJSON(http.MethodGet, ctrlURL+"/connections", secret, nil, &payload); err != nil {
		return out, err
	}
	out.DownloadTotal = payload.DownloadTotal
	out.UploadTotal = payload.UploadTotal
	out.Memory = payload.Memory
	if payload.Connections != nil {
		for i := range payload.Connections {
			if payload.Connections[i].Chains == nil {
				payload.Connections[i].Chains = []string{}
			}
		}
		out.Connections = payload.Connections
	}
	return out, nil
}

func (m *mihomoManager) CloseConnection(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("缺少连接 ID")
	}
	m.mu.Lock()
	running := m.running
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()
	if !running || ctrlURL == "" {
		return errors.New("Mihomo 内核未运行")
	}
	return m.doControllerJSON(http.MethodDelete, ctrlURL+"/connections/"+url.PathEscape(id), secret, nil, nil)
}

func (m *mihomoManager) CloseAllConnections() error {
	m.mu.Lock()
	running := m.running
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()
	if !running || ctrlURL == "" {
		return errors.New("Mihomo 内核未运行")
	}
	return m.doControllerJSON(http.MethodDelete, ctrlURL+"/connections", secret, nil, nil)
}
