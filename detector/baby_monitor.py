#!/usr/bin/env python3
"""
Lightweight Baby Cry Detector for Raspberry Pi
Uses the OpenBabyMonitor ONNX neural network model to detect baby crying
from a USB microphone. Plays a beep alert when crying is detected.
No web server, no database — just audio detection and alerts.
"""

import sys
import os
import time
import subprocess
import re
import collections
import argparse
import numpy as np
import cv2

# Add the OpenBabyMonitor detection module
OBM_DIR = os.environ.get("BABY_MONITOR_MODEL_DIR", os.path.expanduser("~/OpenBabyMonitor"))
# Look for feature extraction library in model dir, then bundled lib/
_det_path = os.path.join(OBM_DIR, "detection")
_lib_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib")
if os.path.isdir(_det_path):
    sys.path.insert(0, _det_path)
else:
    sys.path.insert(0, _lib_path)
import features
import librosa_destilled
import struct


# --- PulseAudio Recorder (for Bluetooth / PipeWire sources) ---

class PulseRecorder:
    """Records audio via PulseAudio using parec. Used for Bluetooth devices."""

    def __init__(self, source, sampling_rate=8000, amplification=1):
        self.source = source
        self.sampling_rate = sampling_rate
        self.amplification = amplification
        self.parec_args = [
            "parec",
            "--device", source,
            "--rate", str(sampling_rate),
            "--channels", "1",
            "--format", "float32le",
            "--raw",
        ]

    def record_waveform(self, n_samples):
        """Record n_samples from PulseAudio source, return (waveform, record_time)."""
        # Calculate bytes: float32 = 4 bytes per sample
        n_bytes = n_samples * 4
        try:
            proc = subprocess.Popen(
                self.parec_args,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
            )
            record_time = time.time() + 0.5 * n_samples / self.sampling_rate
            raw = proc.stdout.read(n_bytes)
            proc.terminate()
            proc.wait(timeout=2)
        except Exception as e:
            print(f"PulseAudio recording error: {e}")
            return np.zeros(n_samples, dtype=np.float32), time.time()

        if len(raw) < n_bytes:
            # Pad with silence if short
            raw = raw + b'\x00' * (n_bytes - len(raw))

        waveform = np.frombuffer(raw, dtype=np.float32).copy()
        waveform *= self.amplification
        return waveform, record_time


# --- Audio device helpers ---

def find_mic():
    """Auto-detect USB microphone device ID."""
    output = subprocess.check_output(["arecord", "-l"], text=True)
    matches = re.findall(
        r"^card (\d+): .*, device (\d+): .*$", output, flags=re.MULTILINE
    )
    if not matches:
        print("ERROR: No microphone found. Plug in a USB mic and retry.")
        sys.exit(1)
    # Pick the last match (usually the USB device, not onboard)
    card, device = matches[-1]
    mic_id = f"hw:{card},{device}"
    print(f"Detected microphone: {mic_id}")
    return mic_id


def set_mic_volume(mic_id, volume=100):
    """Set microphone capture volume to max."""
    card = mic_id.split(":")[1].split(",")[0]
    try:
        output = subprocess.check_output(["amixer", "-c", card], text=True)
        controls = re.findall(
            r"^.+ '(.+)',\d+$\n^  Capabilities: .*c?volume.*$",
            output,
            flags=re.MULTILINE,
        )
        if controls:
            subprocess.run(
                ["amixer", "-c", card, "sset", controls[0], f"{volume}%"],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            print(f"Mic volume set to {volume}%")
    except Exception as e:
        print(f"Warning: Could not set mic volume: {e}")


# --- ONNX Model ---

class CryModel:
    """Loads ONNX cry detection model via OpenCV DNN."""

    LABELS = {0: "ambient", 1: "crying", 2: "babbling"}

    def __init__(self, model_path):
        if not os.path.exists(model_path):
            print(f"ERROR: Model not found at {model_path}")
            sys.exit(1)
        self.model = cv2.dnn.readNetFromONNX(model_path)
        print(f"Loaded model: {model_path}")

    def predict(self, feature):
        """Run inference on a mel-spectrogram feature. Returns probabilities."""
        self.model.setInput(feature[np.newaxis, np.newaxis, :, :])
        raw = self.model.forward().squeeze()
        probs = 10**raw
        probs /= np.sum(probs)
        return probs


# --- Notifier ---

class CryNotifier:
    """Tracks predictions over a sliding window and triggers alerts.

    Uses a sliding window of the last `window_size` inference results
    (quiet/skipped checks are not counted). Fires an alert when the
    ratio of crying detections in the window >= `window_ratio`.

    This handles intermittent crying (cry-pause-cry-pause) that would
    reset a simple consecutive-streak counter, while still preventing
    false positives from a single spurious detection.
    """

    def __init__(
        self,
        consecutive=6,
        probability_threshold=0.80,
        min_interval=180,
        window_ratio=0.50,
        **kwargs,  # ignore unused params from config
    ):
        self.window_size = consecutive  # reuse consecutive param as window size
        self.probability_threshold = probability_threshold
        self.min_interval = min_interval
        self.window_ratio = window_ratio
        self.window = []  # list of bools: True=crying, False=not
        self.streak = 0   # kept for display/logging
        self.consecutive = consecutive  # kept for display/logging
        self.last_alert_time = -float("inf")

    def add(self, probs):
        """Add prediction. Returns True if alert should fire."""
        cry_prob = probs[1]  # index 1 = crying
        is_crying = cry_prob >= self.probability_threshold

        # Update streak for logging
        if is_crying:
            self.streak += 1
        else:
            self.streak = 0

        # Add to sliding window
        self.window.append(is_crying)
        if len(self.window) > self.window_size:
            self.window.pop(0)

        # Check if enough of the window is crying
        if len(self.window) < min(3, self.window_size):
            # Need at least 3 samples (or window_size if smaller) before alerting
            return False

        cry_count = sum(self.window)
        cry_ratio = cry_count / len(self.window)

        now = time.time()
        if cry_ratio >= self.window_ratio and (now - self.last_alert_time) > self.min_interval:
            self.last_alert_time = now
            self.window.clear()
            return True
        return False


# --- Alert ---

def play_alert():
    """Play a beep sound using aplay or speaker-test."""
    try:
        # Generate a short beep using ffmpeg
        subprocess.run(
            [
                "ffmpeg", "-y", "-f", "lavfi", "-i",
                "sine=frequency=800:duration=0.5",
                "-f", "wav", "/tmp/alert.wav",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        subprocess.run(
            ["aplay", "/tmp/alert.wav"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    except Exception:
        print("\a")  # Terminal bell fallback


def _find_model_file(filename):
    """Look for model file in OBM_DIR/control/ then OBM_DIR/"""
    p = os.path.join(OBM_DIR, "control", filename)
    if os.path.exists(p):
        return p
    return os.path.join(OBM_DIR, filename)


# --- Main loop ---

def main():
    parser = argparse.ArgumentParser(description="Baby Cry Detector")
    parser.add_argument(
        "--model",
        default=_find_model_file("crynet_large.onnx"),
        help="Path to ONNX model",
    )
    parser.add_argument(
        "--standardization",
        default=_find_model_file("standardization.npz"),
        help="Path to standardization.npz",
    )
    parser.add_argument("--interval", type=float, default=5.0, help="Seconds between checks")
    parser.add_argument("--amplification", type=float, default=10.0, help="Audio amplification")
    parser.add_argument("--min-contrast", type=float, default=15.0, help="Min sound contrast over background")
    parser.add_argument("--consecutive", type=int, default=10, help="Sliding window size: number of recent detections to track")
    parser.add_argument("--fraction", type=float, default=0.50, help="Alert when this ratio of the window is crying (0.0-1.0)")
    parser.add_argument("--prob-threshold", type=float, default=0.80, help="Probability threshold for crying")
    parser.add_argument("--cooldown", type=int, default=180, help="Min seconds between alerts")
    parser.add_argument("--threshold-only", action="store_true", help="Use simple loudness threshold (no NN)")
    parser.add_argument("--mic-device", type=str, default="", help="Mic device: hw:X,Y for ALSA, pulse:source_name for PulseAudio/Bluetooth")
    args = parser.parse_args()

    # Detect mic
    use_pulse = False
    if args.mic_device and args.mic_device.startswith("pulse:"):
        # PulseAudio source (Bluetooth, PipeWire, etc.)
        pulse_source = args.mic_device[len("pulse:"):]
        audio_device = pulse_source
        use_pulse = True
        print(f"Using PulseAudio source: {pulse_source}")
    elif args.mic_device:
        mic_id = args.mic_device
        print(f"Using specified microphone: {mic_id}")
        set_mic_volume(mic_id)
        audio_device = mic_id.replace("hw:", "plughw:")
    else:
        mic_id = find_mic()
        set_mic_volume(mic_id)
        audio_device = mic_id.replace("hw:", "plughw:")

    # Set up feature extractor
    feature_extractor = features.AudioFeatureExtractor(
        n_mel_bands=64,
        feature_window_count=128,
        backend="python_speech_features",
        disable_io=True,
    )
    feature_extractor.create_A_weights()

    # Set up recorder
    if use_pulse:
        recorder = PulseRecorder(
            audio_device,
            sampling_rate=feature_extractor.sampling_rate,
            amplification=args.amplification,
        )
    else:
        recorder = features.Recorder(
            audio_device,
            sampling_rate=feature_extractor.sampling_rate,
            amplification=args.amplification,
        )

    # Set up loudness analyzer
    analyzer = features.LoudnessAnalyzer()

    # Load standardization
    standardizer = None
    if not args.threshold_only and os.path.exists(args.standardization):
        standardizer = features.Standardizer(args.standardization)
        print(f"Loaded standardization: {args.standardization}")

    # Load model (unless threshold-only mode)
    model = None
    notifier = None
    if not args.threshold_only:
        model = CryModel(args.model)
        notifier = CryNotifier(
            consecutive=args.consecutive,
            probability_threshold=args.prob_threshold,
            min_interval=args.cooldown,
            window_ratio=args.fraction,
        )

    print()
    print("=" * 50)
    if args.threshold_only:
        print("  Baby Cry Detector (loudness threshold mode)")
    else:
        print("  Baby Cry Detector (neural network mode)")
    print(f"  Checking every {args.interval}s | Cooldown: {args.cooldown}s")
    print("=" * 50)
    print()
    print("Starting audio capture... (Ctrl+C to stop)")
    print()

    loud_count = 0
    ready = False

    try:
        while True:
            start = time.time()

            # Record audio
            waveform, record_time = recorder.record_waveform(
                feature_extractor.feature_length
            )

            # Extract features and loudness
            feature, loudness = feature_extractor.compute_feature_and_loudness(waveform)
            if loudness is None or not hasattr(loudness, 'ndim') or loudness.ndim == 0 or loudness.size == 0:
                continue
            if not ready:
                # Machine-readable readiness marker. Emitting this only after a
                # successful capture avoids claiming readiness while the model
                # is loading or an audio device is failing to produce samples.
                print("BABY_MONITOR_READY")
                ready = True
            analyzer.add_loudness(loudness)
            bg_level, signal_level = analyzer.get_loudness_levels()

            timestamp = time.strftime("%H:%M:%S")

            if args.threshold_only:
                # Simple threshold mode
                is_loud = signal_level >= args.min_contrast
                if is_loud:
                    loud_count += 1
                else:
                    loud_count = 0

                status = "LOUD" if is_loud else "quiet"
                print(f"[{timestamp}] bg={bg_level:+.1f}dB  signal={signal_level:+.1f}dB  {status}")

                if loud_count >= args.consecutive:
                    print(f"[{timestamp}] *** ALERT: Sustained loud sound detected! ***")
                    play_alert()
                    loud_count = 0
            else:
                # Neural network mode
                if signal_level < args.min_contrast:
                    print(f"[{timestamp}] bg={bg_level:+.1f}dB  signal={signal_level:+.1f}dB  [quiet - skipping inference]")
                else:
                    # Amplify and standardize
                    feature = recorder.amplify_feature(feature)
                    if standardizer:
                        standardizer(feature)

                    probs = model.predict(feature)
                    label_idx = np.argmax(probs)
                    label = CryModel.LABELS[label_idx]

                    cry_in_window = sum(notifier.window) + (1 if probs[1] >= notifier.probability_threshold else 0)
                    window_len = min(len(notifier.window) + 1, notifier.window_size)
                    print(
                        f"[{timestamp}] bg={bg_level:+.1f}dB  signal={signal_level:+.1f}dB  "
                        f"ambient={probs[0]:.0%}  CRYING={probs[1]:.0%}  babbling={probs[2]:.0%}  -> {label}"
                        f"  streak={notifier.streak}/{notifier.consecutive}"
                        f"  window={cry_in_window}/{window_len}"
                    )

                    alert = notifier.add(probs)
                    if alert:
                        print(f"[{timestamp}] *** ALERT: Baby is crying! ({cry_in_window}/{window_len} detections in window) ***")
                        play_alert()

            # Wait for next interval
            elapsed = time.time() - start
            time.sleep(max(0, args.interval - elapsed))

    except KeyboardInterrupt:
        print("\nStopped.")


if __name__ == "__main__":
    main()
