package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type GeminiRequest struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *SystemInstruction `json:"systemInstruction,omitempty"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type SystemInstruction struct {
	Parts []Part `json:"parts"`
}

type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content Content `json:"content"`
}

// DiagnoseIncident calls the Gemini API to get a root cause analysis and resolution playbook.
func DiagnoseIncident(ctx context.Context, title, device, payload string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "⚠️ Co-Pilot AI Diagnostics: A chave GEMINI_API_KEY não está configurada nas variáveis de ambiente do backend.", nil
	}

	systemPrompt := "Você é um Engenheiro de Confiabilidade (SRE) e Analista de Segurança (SOC) especialista global. " +
		"Analise o incidente e retorne um relatório estruturado em Markdown contendo: " +
		"1. Análise Causa Raiz (Explicação clara do problema). " +
		"2. Passos sugeridos para Resolução/Mitigação rápida. " +
		"3. Recomendações de Prevenção a longo prazo. " +
		"Seja direto, técnico e profissional. Use formatação limpa do markdown."

	userText := fmt.Sprintf("Incidente: %s\nDispositivo/Ativo: %s\nPayload Completo do Alerta:\n%s", title, device, payload)

	reqBody := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{{Text: userText}},
			},
		},
		SystemInstruction: &SystemInstruction{
			Parts: []Part{{Text: systemPrompt}},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=%s", apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("⚠️ Falha ao se conectar com a API do Gemini (HTTP %d)", resp.StatusCode), nil
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return geminiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "⚠️ Nenhum diagnóstico retornado pela IA.", nil
}
