package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"phoenix/internal/config"
	"phoenix/internal/secrets"
	"phoenix/internal/voice"
)

const (
	maxVoiceClip         = 20 * time.Second
	maxVoiceSummaryChars = 500
)

var (
	voiceVault    *secrets.Store
	voiceProvider voice.Provider
	voiceBudget   *voice.Budget
	voiceInitErr  error
)

func initVoice(vault *secrets.Store, configPath string) {
	voiceVault = vault
	voiceBudget = voice.NewBudget(filepath.Dir(configPath))
	voiceProvider, voiceInitErr = voice.New("cartesia")
}

func voiceModeEnabled() bool {
	enabled := false
	if cfgStore != nil {
		cfgStore.View(func(c *config.Config) { enabled = c.Voice.Enabled })
	}
	return enabled
}

func voiceReadyForSummary() bool {
	if !voiceModeEnabled() || voiceProvider == nil || voiceVault == nil || !voiceVault.Has("voice-cartesia") || voiceBudget == nil {
		return false
	}
	_, remaining, err := voiceBudget.Usage()
	return err == nil && remaining >= maxVoiceSummaryChars
}

func handleVoiceOp(f []string) {
	if len(f) < 2 {
		return
	}
	switch f[1] {
	case "status":
		pushVoiceSettings("")
	case "mode":
		if cfgStore == nil || len(f) < 3 || (f[2] != "0" && f[2] != "1") {
			pushVoiceSettings("Voice settings could not be saved.")
			return
		}
		err := cfgStore.Update(func(c *config.Config) { c.Voice.Enabled = f[2] == "1" })
		if err != nil {
			pushVoiceSettings("Voice settings could not be saved.")
			return
		}
		pushVoiceSettings("")
	case "key":
		if voiceVault == nil || len(f) < 3 {
			pushVoiceSettings("Cartesia key could not be saved.")
			return
		}
		key := strings.TrimSpace(f[2])
		if len(key) < 20 || len(key) > 512 || !strings.HasPrefix(key, "sk_car_") {
			pushVoiceSettings("Enter a Cartesia API key beginning with sk_car_.")
			return
		}
		if err := voiceVault.Set("voice-cartesia", key); err != nil {
			pushVoiceSettings("Cartesia key could not be saved to the Windows vault.")
			return
		}
		pushVoiceSettings("Cartesia API key saved in the Windows vault.")
	case "clear":
		if voiceVault == nil {
			pushVoiceSettings("Cartesia key could not be removed.")
			return
		}
		if err := voiceVault.Delete("voice-cartesia"); err != nil {
			pushVoiceSettings("Cartesia key could not be removed.")
			return
		}
		pushVoiceSettings("Cartesia API key removed.")
	case "transcribe":
		if len(f) < 5 {
			postVoiceError("The recording could not be read.")
			return
		}
		startVoiceTranscription(f[2], f[3], f[4])
	case "speak":
		if len(f) < 3 {
			return
		}
		startVoiceSpeech(f[2])
	}
}

func pushVoiceSettings(message string) {
	if !ready {
		return
	}
	enabled := voiceModeEnabled()
	configured := voiceVault != nil && voiceVault.Has("voice-cartesia")
	status := message
	if status == "" {
		switch {
		case voiceInitErr != nil:
			status = "Voice service could not be initialized."
		case !configured:
			status = "Add a Cartesia API key to use voice mode."
		default:
			status = "Cartesia API key is saved in the Windows vault."
		}
	}
	post(
		"u"+fs+"voice-mode"+fs+"checked"+fs+jsonString(enabled),
		"u"+fs+"cartesia-clear"+fs+"disabled"+fs+jsonString(!configured),
		"t"+fs+"voice-status"+fs+status,
	)
	if voiceBudget == nil {
		post("t" + fs + "voice-budget" + fs + "Local credit limit is unavailable; voice requests are disabled.")
		return
	}
	used, remaining, err := voiceBudget.Usage()
	if err != nil {
		post("t" + fs + "voice-budget" + fs + "Local credit limit is unavailable; voice requests are disabled.")
		return
	}
	post("t" + fs + "voice-budget" + fs + fmt.Sprintf("Phoenix usage: %s / 20,000 credits in the last 28 days (%s remaining).", formatCredit(used), formatCredit(remaining)))
}

func formatCredit(n int) string {
	if n >= 1000 {
		return strconv.Itoa(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
	}
	return strconv.Itoa(n)
}

func cartesiaKey() (string, error) {
	if voiceInitErr != nil {
		return "", errorsVoiceUnavailable
	}
	if voiceVault == nil || voiceProvider == nil {
		return "", errorsVoiceUnavailable
	}
	key, err := voiceVault.Get("voice-cartesia")
	if err != nil || key == "" {
		return "", fmt.Errorf("add a Cartesia API key in Settings → General")
	}
	return key, nil
}

var errorsVoiceUnavailable = errors.New("voice service is unavailable")

func reserveVoiceCredits(credits int) error {
	if voiceBudget == nil {
		return fmt.Errorf("local credit limit is unavailable; voice requests are disabled")
	}
	_, _, err := voiceBudget.Reserve(credits)
	if err != nil {
		if err == voice.ErrBudgetExceeded {
			return err
		}
		return fmt.Errorf("local credit limit is unavailable; voice requests are disabled")
	}
	return nil
}

func startVoiceTranscription(encoded, contentType, durationText string) {
	if !voiceModeEnabled() {
		postVoiceError("Turn on voice mode in Settings to dictate.")
		return
	}
	if len(encoded) > 3<<20 || !(strings.HasPrefix(contentType, "audio/webm") || strings.HasPrefix(contentType, "audio/ogg")) {
		postVoiceError("Recording format or size is not supported.")
		return
	}
	durationMS, err := strconv.Atoi(durationText)
	if err != nil || durationMS < 1 || durationMS > int(maxVoiceClip/time.Millisecond) {
		postVoiceError("Recordings are limited to 20 seconds.")
		return
	}
	audio, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(audio) == 0 || len(audio) > 2<<20 {
		postVoiceError("Recording format or size is not supported.")
		return
	}
	key, err := cartesiaKey()
	if err != nil {
		postVoiceError(err.Error())
		return
	}
	credits := 3 * ((durationMS + 999) / 1000)
	if err := reserveVoiceCredits(credits); err != nil {
		postVoiceError(err.Error())
		return
	}
	postVoiceUsage()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		text, err := voiceProvider.Transcribe(ctx, key, audio, contentType)
		runLater(func() {
			if err != nil {
				logf("Cartesia speech recognition failed: %v", err)
				postVoiceError(speechError(err))
				return
			}
			postChat(chatEvent{K: "voice-transcript", Text: clippedUTF8(text, 4000)})
		})
	}()
}

func startVoiceSpeech(text string) {
	text = strings.TrimSpace(text)
	if !voiceModeEnabled() || text == "" {
		return
	}
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxVoiceSummaryChars {
		postVoiceError("Spoken summaries must be under 500 characters.")
		return
	}
	key, err := cartesiaKey()
	if err != nil {
		postVoiceError(err.Error())
		return
	}
	if err := reserveVoiceCredits(utf8.RuneCountInString(text)); err != nil {
		postVoiceError(err.Error())
		return
	}
	postVoiceUsage()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		audio, err := voiceProvider.Speak(ctx, key, text)
		runLater(func() {
			if err != nil {
				logf("Cartesia speech generation failed: %v", err)
				postVoiceError(speechError(err))
				return
			}
			postChat(chatEvent{K: "voice-audio", Text: base64.StdEncoding.EncodeToString(audio)})
		})
	}()
}

func postVoiceUsage() {
	if voiceBudget == nil {
		return
	}
	used, remaining, err := voiceBudget.Usage()
	if err == nil {
		post("t" + fs + "voice-budget" + fs + fmt.Sprintf("Phoenix usage: %s / 20,000 credits in the last 28 days (%s remaining).", formatCredit(used), formatCredit(remaining)))
	}
}

func postVoiceError(message string) {
	postChat(chatEvent{K: "voice-error", Text: message})
}

func speechError(err error) string {
	switch {
	case strings.Contains(err.Error(), "rejected the API key"):
		return "Cartesia rejected the API key. Check it in Settings → General."
	case strings.Contains(err.Error(), "rate limit or free credits reached"):
		return "Cartesia's free credits or request limit have been reached. Phoenix will not retry."
	case strings.Contains(err.Error(), voice.ErrBudgetExceeded.Error()):
		return "Phoenix's 20,000-credit free-use limit is reached."
	default:
		return "Cartesia could not complete the voice request. Check your connection and API key."
	}
}
