import os
import threading
import tempfile
from flask import Flask, request, jsonify
from flask_cors import CORS
from faster_whisper import WhisperModel

app = Flask(__name__)
CORS(app)

MODEL_SIZE = os.environ.get("WHISPER_MODEL", "tiny")
LANGUAGE = os.environ.get("WHISPER_LANG", "zh")

# Biases `tiny` toward Simplified Chinese + shell/dev vocabulary. Without this it
# happily emits Traditional characters and mangles command names.
INITIAL_PROMPT = os.environ.get(
    "WHISPER_PROMPT",
    "以下是简体中文的开发命令与技术对话：git 提交，docker 重启容器，"
    "查看日志，npm 安装依赖，python 脚本，端口，配置文件，服务器。"
)

# `tiny` produces these verbatim from silence or noise. Drop them wholesale.
HALLUCINATIONS = (
    "谢谢观看", "感谢观看", "请不吝点赞", "订阅", "转发", "打赏",
    "明镜与点点栏目", "字幕由", "字幕组", "谢谢大家", "谢谢收看",
    "MING PAI", "小编", "本视频", "关注我",
)


def clean(text):
    """Strip whitespace and reject known hallucination boilerplate."""
    t = (text or "").strip()
    if not t:
        return ""
    bare = t.strip("。，、！？.,!? 　")
    for bad in HALLUCINATIONS:
        if bad in t and len(bare) <= len(bad) + 4:
            return ""
    # A single repeated character ("啊啊啊啊啊") is decoder noise, not speech.
    if len(bare) >= 4 and len(set(bare)) == 1:
        return ""
    return t


print("Loading Whisper model (%s)..." % MODEL_SIZE)
model = WhisperModel(MODEL_SIZE, device="cpu", compute_type="int8")
# faster-whisper is not safe for concurrent transcribe() calls on one model.
model_lock = threading.Lock()
print("Model loaded.")


@app.route('/health', methods=['GET'])
def health():
    return jsonify({"ok": True, "model": MODEL_SIZE, "lang": LANGUAGE})


@app.route('/transcribe', methods=['POST'])
def transcribe():
    if 'audio' not in request.files:
        return jsonify({"error": "No audio file provided"}), 400

    audio_file = request.files['audio']

    # Keep the client's container extension so ffmpeg gets a demuxer hint.
    suffix = os.path.splitext(audio_file.filename or "")[1].lower()
    if suffix not in (".webm", ".mp4", ".m4a", ".ogg", ".wav"):
        suffix = ".webm"

    with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as tmp:
        tmp_path = tmp.name
    audio_file.save(tmp_path)

    try:
        if os.path.getsize(tmp_path) == 0:
            return jsonify({"text": ""})

        with model_lock:
            segments, _info = model.transcribe(
                tmp_path,
                language=LANGUAGE,
                beam_size=1,                    # greedy: much lower latency on CPU
                vad_filter=True,                # drop silence before decoding
                vad_parameters={"min_silence_duration_ms": 300},
                condition_on_previous_text=False,  # avoid repetition loops
                # The client now sends one short utterance per request, so the
                # model gets no context. These keep `tiny` from inventing text
                # out of room noise and breath.
                temperature=0.0,
                no_speech_threshold=0.6,
                log_prob_threshold=-1.0,
                compression_ratio_threshold=2.4,
                initial_prompt=INITIAL_PROMPT,
            )
            text = "".join(seg.text for seg in segments)
        return jsonify({"text": clean(text)})
    except Exception as e:
        app.logger.warning("transcribe failed: %s", e)
        return jsonify({"error": str(e)}), 500
    finally:
        try:
            os.remove(tmp_path)
        except OSError:
            pass


if __name__ == '__main__':
    app.run(host='127.0.0.1', port=30022, threaded=True)
