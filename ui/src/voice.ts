import { icon } from "./icons";
import { el } from "./dom";
import { send } from "./protocol";

const MAX_RECORDING_MS = 20_000;
const activeControls = new Set<VoiceControl>();
let playbackContext: AudioContext | null = null;
let currentPlayback: AudioBufferSourceNode | null = null;

function modeInput(): HTMLInputElement | null {
  return document.querySelector<HTMLInputElement>("#voice-mode");
}

export function isVoiceModeEnabled(): boolean {
  return modeInput()?.checked ?? false;
}

export function syncVoiceMode(): void {
  activeControls.forEach((control) => control.syncMode());
}

function unlockPlayback(): void {
  if (!playbackContext) playbackContext = new AudioContext();
  void playbackContext.resume();
}

export function prepareVoicePlayback(): void {
  if (isVoiceModeEnabled()) unlockPlayback();
}

export function playVoiceAudio(encoded: string): void {
  if (!isVoiceModeEnabled() || !encoded) return;
  unlockPlayback();
  const binary = atob(encoded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  void playbackContext!.decodeAudioData(bytes.buffer).then((buffer) => {
    currentPlayback?.stop();
    const source = playbackContext!.createBufferSource();
    source.buffer = buffer;
    source.connect(playbackContext!.destination);
    currentPlayback = source;
    source.onended = () => { if (currentPlayback === source) currentPlayback = null; };
    source.start();
  }).catch(() => showVoiceStatus("Cartesia audio could not be played."));
}

export function finishVoiceRequest(): void {
  activeControls.forEach((control) => control.setBusy(false));
}

export function showVoiceStatus(message: string): void {
  const status = document.querySelector<HTMLElement>("#voice-status");
  if (status) status.textContent = message;
  activeControls.forEach((control) => control.setStatus(message));
}

export function initVoiceSettings(): void {
  const mode = modeInput();
  mode?.addEventListener("change", () => {
    send("w", "mode", mode.checked ? "1" : "0");
    activeControls.forEach((control) => control.syncMode());
    if (mode.checked) unlockPlayback();
    else {
      currentPlayback?.stop();
      currentPlayback = null;
    }
  });
  document.querySelector<HTMLButtonElement>("#cartesia-save")?.addEventListener("click", () => {
    const key = document.querySelector<HTMLInputElement>("#cartesia-api-key");
    if (!key?.value.trim()) {
      showVoiceStatus("Paste a Cartesia API key first.");
      return;
    }
    send("w", "key", key.value);
    key.value = "";
    showVoiceStatus("Saving Cartesia API key…");
  });
  document.querySelector<HTMLButtonElement>("#cartesia-clear")?.addEventListener("click", () => {
    send("w", "clear");
    showVoiceStatus("Removing Cartesia API key…");
  });
  send("w", "status");
}

export function createVoiceButton(): HTMLButtonElement {
  const control = new VoiceControl();
  activeControls.add(control);
  return control.button;
}

class VoiceControl {
  readonly button = el("button", "share-chip");
  #recorder: MediaRecorder | null = null;
  #stream: MediaStream | null = null;
  #chunks: Blob[] = [];
  #timer = 0;
  #startedAt = 0;
  #busy = false;
  #status = "Dictate a message with Cartesia";
  #cancelled = false;

  constructor() {
    this.button.type = "button";
    this.button.setAttribute("aria-label", this.#status);
    this.button.addEventListener("click", () => void this.toggle(), { passive: true });
    this.syncMode();
  }

  syncMode(): void {
    this.button.hidden = !isVoiceModeEnabled();
    if (!isVoiceModeEnabled()) this.cancelRecording();
    this.#render();
  }

  setBusy(busy: boolean): void {
    this.#busy = busy;
    this.#render();
  }

  setStatus(message: string): void {
    this.#status = message;
    if (!this.#recorder) this.button.title = message;
  }

  async toggle(): Promise<void> {
    if (!isVoiceModeEnabled() || this.#busy) return;
    if (this.#recorder) {
      this.#recorder.stop();
      return;
    }
    if (!navigator.mediaDevices?.getUserMedia || !window.MediaRecorder) {
      showVoiceStatus("Audio recording is unavailable in this WebView.");
      return;
    }
    this.#busy = true;
    this.#render();
    try {
      unlockPlayback();
      this.#stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (!isVoiceModeEnabled()) {
        this.releaseStream();
        this.#busy = false;
        this.#render();
        return;
      }
      const mimeType = ["audio/webm;codecs=opus", "audio/ogg;codecs=opus"].find((type) => MediaRecorder.isTypeSupported(type));
      this.#recorder = mimeType ? new MediaRecorder(this.#stream, { mimeType }) : new MediaRecorder(this.#stream);
      this.#chunks = [];
      this.#startedAt = performance.now();
      this.#cancelled = false;
      this.#recorder.addEventListener("dataavailable", (event) => {
        if (event.data.size) this.#chunks.push(event.data);
      });
      this.#recorder.addEventListener("stop", () => void this.finishRecording(), { once: true });
      this.#recorder.start();
      this.#busy = false;
      this.#timer = window.setTimeout(() => this.#recorder?.stop(), MAX_RECORDING_MS);
      this.#render();
      showVoiceStatus("Recording… click the mic to send, up to 20 seconds.");
    } catch {
      this.cancelRecording();
      this.#busy = false;
      this.#render();
      showVoiceStatus("Microphone access was denied or is unavailable.");
    }
  }

  cancelRecording(): void {
    this.#cancelled = true;
    window.clearTimeout(this.#timer);
    if (this.#recorder && this.#recorder.state !== "inactive") this.#recorder.stop();
    else this.releaseStream();
  }

  async finishRecording(): Promise<void> {
    window.clearTimeout(this.#timer);
    const recorder = this.#recorder;
    this.#recorder = null;
    this.releaseStream();
    this.#busy = true;
    this.#render();
    const duration = Math.min(MAX_RECORDING_MS, Math.max(1, Math.ceil(performance.now() - this.#startedAt)));
    const blob = new Blob(this.#chunks, { type: recorder?.mimeType || "audio/webm" });
    this.#chunks = [];
    if (this.#cancelled) {
      this.#busy = false;
      this.#render();
      return;
    }
    if (blob.size === 0 || blob.size > 2 * 1024 * 1024) {
      this.#busy = false;
      this.#render();
      showVoiceStatus("Recording was empty or too large. Try a shorter clip.");
      return;
    }
    try {
      const data = new Uint8Array(await blob.arrayBuffer());
      let binary = "";
      for (let i = 0; i < data.length; i += 0x8000) {
        binary += String.fromCharCode(...data.subarray(i, i + 0x8000));
      }
      send("w", "transcribe", btoa(binary), blob.type || "audio/webm", String(duration));
      showVoiceStatus("Transcribing with Cartesia…");
    } catch {
      this.#busy = false;
      this.#render();
      showVoiceStatus("The recording could not be sent.");
    }
  }

  releaseStream(): void {
    this.#stream?.getTracks().forEach((track) => track.stop());
    this.#stream = null;
  }

  #render(): void {
    const recording = this.#recorder?.state === "recording";
    this.button.disabled = this.#busy;
    this.button.dataset["listening"] = String(recording);
    this.button.title = recording ? "Stop recording and transcribe" : this.#status;
    this.button.setAttribute("aria-label", this.button.title);
    this.button.replaceChildren(icon(recording ? "stop" : "mic", 14), el("span", undefined, recording ? "Stop" : "Speak"));
  }
}
