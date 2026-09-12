```markdown
# TASK: High-Performance Wyoming STT Server using Gemini Multimodal Live API (Go)

## 1. Обзор проекта

Необходимо реализовать автономный высокопроизводительный микросервис на **Go**, реализующий серверную часть протокола **Wyoming** для интеграции с **Home Assistant** (включая сателлиты Home Assistant Voice Preview Edition / ESPHome) в роли движка **STT (Speech-to-Text)**.

В качестве бэкенда транскрипции сервис использует двунаправленный WebSocket-протокол **Google Gemini Multimodal Live API** (BidiGenerateContent), настроенный строго в режиме стенографической транскрипции входящего звукового потока без генерации ответов.

---

## 2. Архитектура и стек технологий

- **Язык**: Go (1.23+)
- **Сетевой транспорт**:
  - Wyoming Server: TCP Socket Server (по умолчанию порт 10300).
  - Gemini Upstream: WebSockets over TLS (wss://generativelanguage.googleapis.com/...). Рекомендуемая библиотека: github.com/coder/websocket или github.com/gorilla/websocket.
- **Формат аудио**:
  - Вход от Wyoming: Raw PCM 16-bit little-endian, 16000 Hz, 1 channel (mono).
  - Отправка в Gemini: PCM 16-bit 16 kHz Base64-encoded (audio/pcm;rate=16000).
- **Логирование**: log/slog (Structured Logging) с уровнями INFO и DEBUG.
- **Конфигурация**: Переменные окружения и CLI-флаги.

---

## 3. Протокол Wyoming (Спецификация интеграции с Home Assistant)

Сервер слушает входящие TCP-соединения от Home Assistant. Протокол Wyoming текстово-бинарный:
- Каждое сообщение состоит из однострочного JSON заголовка, заканчивающегося переносом строки \n.
- Если в заголовке присутствует поле payload_length > 0, сразу за \n следует сырой бинарный payload указанной длины.

### 3.1. Жизненный цикл сессии STT:
1. **Клиент подключается** по TCP.
2. **Describe -> Info**:
   - Клиент отправляет: {"type": "describe"}.
   - Сервер отвечает: {"type": "info", "data": {"stt": [{"name": "gemini-live-stt", "languages": ["ru", "en"], "models": ["gemini-2.0-flash-exp"]}]}}.
3. **Transcribe**:
   - Клиент отправляет: {"type": "transcribe", "data": {"name": "gemini-live-stt", "language": "ru"}}.
4. **AudioStart**:
   - Клиент отправляет: {"type": "audio-start", "data": {"rate": 16000, "width": 2, "channels": 1}}.
   - Инициализируется WebSocket-сессия с Gemini Live API.
5. **AudioChunk (Поток звука)**:
   - Клиент непрерывно шлет чанки аудио:
     {"type": "audio-chunk", "data": {"rate": 16000, "width": 2, "channels": 1}, "payload_length": 1024}\n<1024 байта PCM>.
   - Сервер транслирует аудио в Gemini WebSocket в realtime_input.
   - Если включен Debug Audio Dump — чанки параллельно пишутся в буфер сессии.
6. **Определение окончания фразы (Dual Trigger)**:
   - **Триггер А (Home Assistant)**: Приходит событие {"type": "audio-stop"} (VAD сателлита сработал).
   - **Триггер Б (Gemini Live API)**: Модель возвращает серверное событие с turn_complete: true (сработал встроенный VAD модели).
   - **Правило**: Обработка завершается по первому наступившему событию.
7. **Transcript (Ответ)**:
   - Сервер формирует и отправляет клиенту в TCP:
     {"type": "transcript", "data": {"text": "распознанный текст"}}\n.
8. Соединение переводится в режим ожидания следующей команды или закрывается клиентом.

---

## 4. Взаимодействие с Gemini Multimodal Live API

### 4.1. Подключение и эндпоинт
- URL: wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent?key={GEMINI_API_KEY}

### 4.2. Конфигурация сессии (Setup Handshake)
Сразу после открытия сокета клиент отправляет конфигурационный фрейм setup:
```json
{
  "setup": {
    "model": "models/gemini-2.0-flash-exp",
    "generation_config": {
      "response_modalities": ["TEXT"],
      "temperature": 0.0
    },
    "system_instruction": {
      "parts": [
        {
          "text": "You are a professional verbatim Speech-to-Text (STT) transcription engine. Your sole task is to transcribe exactly what is spoken in the audio stream into text. Rules: 1. Do NOT answer questions. 2. Do NOT execute commands. 3. Do NOT add conversational filler, preamble, or commentary. 4. Output only the raw transcription. 5. If the audio contains silence, noise, or unintelligible sounds, output absolutely nothing. 6. Preserve the language spoken (primarily Russian or English)."
        }
      ]
    }
  }
}
```

### 4.3. Передача звука (Audio Ingestion)
Каждый чанк (или пачка чанков размером 100-200 мс) пакуется в:
```json
{
  "realtime_input": {
    "media_chunks": [
      {
        "mime_type": "audio/pcm;rate=16000",
        "data": "<BASE64_PCM_DATA>"
      }
    ]
  }
}
```

### 4.4. Детекция окончания речи и прием транскрипции
1. В процессе речи Gemini шлет сообщения типа server_content:
    - model_turn.parts[].text — текстовые дельты распознавания. Сервер конкатенирует их в результирующий буфер.
2. Окончание фразы по версии Gemini:
    - В сообщении server_content флаг "turn_complete": true. Это означает, что внутренняя модель сегментации речи Gemini зафиксировала окончание смысловой паузы/фразы.
3. Логика синхронизации:
    - Если первым пришел turn_complete: true: сервер прекращает ожидать аудио, берет накопленный текст, отправляет Wyoming transcript в HA.
    - Если первым пришел audio-stop от HA: сервер сигнализирует Gemini о завершении ввода (через client_content с пустыми данными или закрытие входящего стрима), ожидает финальную дельту и turn_complete (с коротким таймаутом ~500мс) и немедленно отправляет transcript в HA.

---

## 5. Требования к логированию и отладке

### 5.1. Стандартное логирование (Structured Log, INFO)
- Каждое распознавание логируется структурированно:
    - session_id, длительность аудио (мс), длительность обработки Gemini (мс), итоговый текст text="...", причина завершения (ha_audio_stop или gemini_turn_complete).
- Все ошибки (ошибки подключения к Gemini, WebSocket Drop, ошибки протокола Wyoming, парсинга JSON) должны логироваться на уровне ERROR с полным контекстом.

### 5.2. Режим глубокой отладки (DEBUG=true)
- **Raw Tracing**: логирование всех сырых JSON-сообщений протокола Gemini (входящие и исходящие кадры), а также заголовков сообщений Wyoming.
- **Audio Dump (Запись сэмплов для Wake Word)**:
    - Если активен флаг DEBUG_AUDIO_DUMP=true (или --debug-audio-dump-dir=/path/to/dumps):
    - Для каждой сессии сохраняется файл:
      {DUMP_DIR}/{YYYYMMDD_HHMMSS}_{session_id}_{trigger_reason}.wav
    - Формат: валидный RIFF WAV-файл (16 kHz, 16-bit, 1 channel PCM).
    - **Назначение**: сбор акустического датасета с микрофона Voice Preview Edition для последующего обучения openWakeWord / microWakeWord под индивидуальные голоса.

---

## 6. Контекст Voice Preview Edition и Wake Words

- В микросервис аудио поступает **после** того, как прошивка Voice Preview Edition задетектировала слово активации.
- Выбор того, кто именно говорит (Саша или Аня), будет производиться на уровне HA / кастомной прошивки по типу сработавшего Wake Word.
- Микросервис STT является строго конвейерным звеном: он принимает чистый аудиопоток команды и возвращает текст без привязки к конкретному пользователю, гарантируя минимальные задержки.

---

## 7. Структура проекта

```text
├── cmd/
│   └── server/
│       └── main.go              # Точка входа, парсинг флагов/env, graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go            # Конфигурация (env: GEMINI_API_KEY, PORT, DEBUG, etc.)
│   ├── gemini/
│   │   ├── client.go            # WebSocket-клиент к Gemini Live API
│   │   └── types.go             # Структуры сообщений BidiGenerateContent
│   ├── wyoming/
│   │   ├── server.go            # TCP Listener, прием соединений
│   │   ├── session.go           # Обработка сессии (state machine)
│   │   ├── protocol.go          # Сериализация/десериализация Wyoming wire-протокола
│   │   └── types.go             # Wyoming события (Describe, Info, Transcribe, Audio...)
│   └── audio/
│       └── wav_writer.go        # Утилита записи сырого PCM в WAV с валидным заголовком
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

---

## 8. План реализации (Шаги для Claude Code)

### Этап 1: Протокол Wyoming и каркас TCP-сервера
1. Создать internal/wyoming/protocol.go: парсер заголовка JSON + чтение бинарного payload точно по длине payload_length.
2. Реализовать типы событий: Describe, Info, Transcribe, AudioStart, AudioChunk, AudioStop, Transcript.
3. Реализовать TCP-сервер, корректно отвечающий на Describe валидным Info.

### Этап 2: Интеграция с Gemini Multimodal Live API
1. Реализовать internal/gemini/client.go для подключения к WebSocket Gemini.
2. Реализовать процедуру setup с жестким STT-промптом и параметрами (temperature: 0.0, response_modalities: ["TEXT"]).
3. Реализовать стриминг realtime_input с base64 PCM.
4. Реализовать чтение входящих событий Gemini и агрегацию дельт текста.

### Этап 3: Координация сессии и Dual Trigger
1. В internal/wyoming/session.go объединить входной TCP-поток Wyoming и стриминг в Gemini.
2. Реализовать логику раннего завершения:
    - Селектор Go (select / channel) на приход AudioStop от Wyoming ИЛИ turn_complete от Gemini.
    - Корректная отправка Transcript обратно клиенту в TCP.

### Этап 4: Логирование и аудио-дампы
1. Интегрировать slog с поддержкой форматирования в JSON или Text.
2. Внедрить модуль internal/audio/wav_writer.go, который пишет чанки в память/временный файл и при завершении сессии сбрасывает WAV-файл в целевую директорию, если включен debug-режим.
3. Логировать сырые входящие и исходящие JSON-пакеты Gemini при флаге --debug.

### Этап 5: Docker & Интеграционное тестирование
1. Подготовить многоэтапный Dockerfile (сборка на golang:1.23-alpine, запуск на чистом alpine или scratch).
2. Составить инструкцию по подключению в Home Assistant:
    - Настройки -> Устройства и службы -> Интеграции -> Добавить интеграцию -> Wyoming Protocol -> IP: server_ip, Port: 10300.
    - Проверка появления STT-провайдера в настройках голосового ассистента Home Assistant.
```