import { icon } from "./icons";
import { el } from "./dom";
import { send } from "./protocol";

const MAX_RECORDING_MS = 20_000;
const activeControls = new Set<VoiceControl>();
let playbackContext: AudioContext | null = null;
let currentPlayback: AudioBufferSourceNode | null = null;
let voiceOwner: VoiceControl | null = null;

export function insertVoiceTranscript(text: string): boolean {
  const target = voiceOwner?.target;
  if (!target) return false;
  if (target.isConnected && !target.disabled) {
    target.value = `${target.value}${target.value ? " " : ""}${text}`.slice(0, target.maxLength > 0 ? target.maxLength : undefined);
    target.dispatchEvent(new Event("input", { bubbles: true }));
    target.focus();
  }
  return true;
}

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
  voiceOwner = null;
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

export function createVoiceButton(target?: HTMLInputElement): HTMLButtonElement {
  activeControls.forEach((control) => { if (!control.button.isConnected && control !== voiceOwner) activeControls.delete(control); });
  const control = new VoiceControl(target);
  activeControls.add(control);
  return control.button;
}

class VoiceControl {
  readonly button = el("button", "share-chip voice-button");
  readonly #wave = el("span", "voice-wave");
  readonly #recordingBar = el("div", "voice-recording-bar");
  readonly #levels = el("div", "voice-levels");
  #recorder: MediaRecorder | null = null;
  #stream: MediaStream | null = null;
  #analyser: AnalyserNode | null = null;
  #meterSource: MediaStreamAudioSourceNode | null = null;
  #meterTimer = 0;
  #lastVoiceAt = 0;
  #speaking = false;
  #chunks: Blob[] = [];
  #timer = 0;
  #startedAt = 0;
  #busy = false;
  #status = "Dictate with Cartesia (Ctrl+Shift+V)";
  #cancelled = false;

  constructor(readonly target?: HTMLInputElement) {
    this.button.type = "button";
    this.#wave.setAttribute("aria-hidden", "true");
    this.#wave.append(el("i"), el("i"), el("i"));
    this.#levels.setAttribute("aria-hidden", "true");
    for (let i = 0; i < 80; i++) this.#levels.append(el("i"));
    const cancel = el("button", "voice-recording-action");
    cancel.type = "button";
    cancel.setAttribute("aria-label", "Cancel recording");
    cancel.append(icon("close", 16));
    cancel.addEventListener("click", () => this.cancelRecording());
    const stop = el("button", "voice-recording-action voice-recording-stop");
    stop.type = "button";
    stop.setAttribute("aria-label", "Stop and transcribe recording");
    stop.append(icon("stop", 16));
    stop.addEventListener("click", () => this.#recorder?.stop());
    this.#recordingBar.append(cancel, this.#levels, stop);
    this.#recordingBar.hidden = true;
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
    if (!isVoiceModeEnabled() || this.#busy || (voiceOwner && voiceOwner !== this) || this.target?.disabled) return;
    if (this.#recorder) {
      this.#recorder.stop();
      return;
    }
    if (!navigator.mediaDevices?.getUserMedia || !window.MediaRecorder) {
      showVoiceStatus("Audio recording is unavailable in this WebView.");
      return;
    }
    this.#busy = true;
    voiceOwner = this;
    this.#render();
    try {
      unlockPlayback();
      this.#stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (!isVoiceModeEnabled()) {
        this.releaseStream();
        this.#busy = false;
        voiceOwner = null;
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
      this.#startVoiceDetection();
      this.#busy = false;
      this.#timer = window.setTimeout(() => this.#recorder?.stop(), MAX_RECORDING_MS);
      this.#render();
      showVoiceStatus("Recording… click the mic to send, up to 20 seconds.");
    } catch {
      this.cancelRecording();
      voiceOwner = null;
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
      voiceOwner = null;
      this.#busy = false;
      this.#render();
      return;
    }
    if (blob.size === 0 || blob.size > 2 * 1024 * 1024) {
      voiceOwner = null;
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
      voiceOwner = null;
      this.#busy = false;
      this.#render();
      showVoiceStatus("The recording could not be sent.");
    }
  }

  releaseStream(): void {
    this.#stopVoiceDetection();
    this.#stream?.getTracks().forEach((track) => track.stop());
    this.#stream = null;
  }

  #stopVoiceDetection(): void {
    window.clearInterval(this.#meterTimer);
    this.#meterTimer = 0;
    this.#meterSource?.disconnect();
    this.#meterSource = null;
    this.#analyser = null;
    this.#lastVoiceAt = 0;
    this.#speaking = false;
  }

  #startVoiceDetection(): void {
    try {
      const analyser = playbackContext!.createAnalyser();
      analyser.fftSize = 512;
      this.#meterSource = playbackContext!.createMediaStreamSource(this.#stream!);
      this.#meterSource.connect(analyser);
      this.#analyser = analyser;
      const samples = new Uint8Array(analyser.fftSize);
      this.#meterTimer = window.setInterval(() => {
        analyser.getByteTimeDomainData(samples);
        let energy = 0;
        for (const sample of samples) {
          const amplitude = (sample - 128) / 128;
          energy += amplitude * amplitude;
        }
        const now = performance.now();
        const level = Math.sqrt(energy / samples.length);
        const first = this.#levels.firstElementChild as HTMLElement | null;
        if (first) {
          first.style.setProperty("--voice-level", String(Math.max(0.08, Math.min(1, level * 9))));
          this.#levels.append(first);
        }
        if (level >= 0.025) this.#lastVoiceAt = now;
        const speaking = this.#lastVoiceAt > 0 && now - this.#lastVoiceAt < 260;
        if (speaking !== this.#speaking) {
          this.#speaking = speaking;
          this.#render();
        }
      }, 80);
    } catch {
      this.#stopVoiceDetection();
    }
  }

  #render(): void {
    const recording = this.#recorder?.state === "recording";
    const composer = this.target ? this.button.closest<HTMLElement>(".chat-question") : this.button.closest<HTMLElement>(".composer");
    if (composer && !this.#recordingBar.isConnected) composer.append(this.#recordingBar);
    this.#recordingBar.hidden = !recording;
    if (composer) composer.dataset["recording"] = String(recording);
    const speaking = recording && this.#speaking;
    this.button.disabled = this.#busy;
    this.button.dataset["listening"] = String(recording);
    this.button.dataset["speaking"] = String(speaking);
    this.button.title = recording ? (speaking ? "Voice detected · click to transcribe" : "Listening · speak or click to stop") : this.#status;
    this.button.setAttribute("aria-label", recording ? "Stop recording and transcribe" : this.#status);
    const actionIcon = icon(recording ? "stop" : "mic", 14);
    actionIcon.toggleAttribute("hidden", speaking);
    this.button.replaceChildren(actionIcon, el("span", undefined, recording ? "Stop" : "Speak"), this.#wave);
  }
}

document.addEventListener("keydown", (event) => {
  if (event.repeat) return;
  if (event.key === "Escape" && voiceOwner && !voiceOwner.button.disabled) {
    voiceOwner.cancelRecording();
    event.preventDefault();
    return;
  }
  if (!event.ctrlKey || !event.shiftKey || event.altKey || event.key.toLowerCase() !== "v") return;
  const focused = document.activeElement;
  const control = voiceOwner ?? [...activeControls].find((item) => item.target === focused && item.button.isConnected)
    ?? [...activeControls].find((item) => !item.target && item.button.isConnected);
  if (!control || !isVoiceModeEnabled()) return;
  event.preventDefault();
  void control.toggle();
});
