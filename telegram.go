package goserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

type TelegramBot struct {
	Token string
}

type PhotoItem struct {
	Path string
}

func NewTelegramBot(token string) *TelegramBot {
	return &TelegramBot{Token: token}
}

func (b *TelegramBot) SendMessage(chatID int64, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.Token)
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	data, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}
	return nil
}

func (b *TelegramBot) SendMessageWithPhoto(chatID int64, text string, photos []PhotoItem) error {
	if len(photos) == 0 {
		return b.SendMessage(chatID, text)
	}

	if len(photos) == 1 {
		p := photos[0]
		if isURL(p.Path) {
			return b.SendPhotoURL(chatID, p.Path, text)
		}
		return b.SendPhoto(chatID, p.Path, text)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMediaGroup", b.Token)

	var media []map[string]string
	for _, p := range photos {
		m := map[string]string{"type": "photo"}
		if isURL(p.Path) {
			m["media"] = p.Path
		} else {
			return fmt.Errorf("несколько локальных файлов в sendMediaGroup пока не поддерживается")
		}
		if text != "" {
			m["caption"] = text
			m["parse_mode"] = "HTML"
		}
		media = append(media, m)
	}

	payload := map[string]any{
		"chat_id": chatID,
		"media":   media,
	}

	data, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}

	return nil
}

func (b *TelegramBot) SendPhotoGallery(chatID int64, photos []PhotoItem) error {
	if len(photos) == 0 {
		return nil
	}
	if len(photos) == 1 {
		p := photos[0]
		if isURL(p.Path) {
			return b.SendPhotoURL(chatID, p.Path, "")
		}
		return b.SendPhoto(chatID, p.Path, "")
	}

	var media []map[string]string
	for _, p := range photos {
		if !isURL(p.Path) {
			return fmt.Errorf("multiple local files in sendMediaGroup are not yet supported")
		}
		m := map[string]string{
			"type":  "photo",
			"media": p.Path,
		}
		media = append(media, m)
	}

	payload := map[string]any{
		"chat_id": chatID,
		"media":   media,
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMediaGroup", b.Token)
	data, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}
	return nil
}

func (b *TelegramBot) SendPhoto(chatID int64, photoPath string, caption string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", b.Token)
	file, err := os.Open(photoPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("failed to close file %s: %v", photoPath, err)
		}
	}()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("chat_id", fmt.Sprintf("%d", chatID)); err != nil {
		return fmt.Errorf("failed to write chat_id field: %w", err)
	}
	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return fmt.Errorf("failed to write caption field: %w", err)
		}
		if err := writer.WriteField("parse_mode", "HTML"); err != nil {
			return fmt.Errorf("failed to write parse_mode field: %w", err)
		}
	}

	part, err := writer.CreateFormFile("photo", filepath.Base(photoPath))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close telegram writer: %w", err)
	}

	req, _ := http.NewRequest("POST", url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}
	return nil
}

func (b *TelegramBot) SendPhotoURL(chatID int64, photoURL string, caption string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", b.Token)
	payload := map[string]any{
		"chat_id": chatID,
		"photo":   photoURL,
	}
	if caption != "" {
		payload["caption"] = caption
		payload["parse_mode"] = "HTML"
	}

	data, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}
	return nil
}

func (b *TelegramBot) SendDocument(chatID int64, docPath string, caption string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", b.Token)
	file, err := os.Open(docPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("failed to close file %s: %v", docPath, err)
		}
	}()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("chat_id", fmt.Sprintf("%d", chatID)); err != nil {
		fmt.Printf("failed to write field chat_id: %v", err)
	}

	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			fmt.Printf("failed to write field caption: %v", err)
		}
		if err := writer.WriteField("parse_mode", "HTML"); err != nil {
			fmt.Printf("failed to write field parse_mode: %v", err)
		}
	}

	part, err := writer.CreateFormFile("document", filepath.Base(docPath))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close telegram writer: %w", err)
	}

	req, _ := http.NewRequest("POST", url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(body))
	}
	return nil
}

func isURL(s string) bool {
	return len(s) > 7 && (s[:7] == "http://" || s[:8] == "https://")
}
