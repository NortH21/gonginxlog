# gonginxlog

CLI-инструмент для парсинга access-логов nginx так, как они реально
настроены — читая настоящую директиву `log_format` (из `nginx.conf`,
либо встроенный дефолт), а не полагаясь на жёстко заданный формат.
Фильтрация по времени, статусу, IP клиента, стране или произвольному
regex, агрегированный отчёт в стиле goaccess, либо живой TUI-дашборд в
стиле k9s (`--ui`) с детекцией аномалий.

## Сборка

```sh
go build -o gonginxlog .
```

(или `go run . <флаги> <файл_лога>` во время разработки)

## Быстрый старт

```sh
# дефолтный main_json (escape=json) формат, полный отчёт
./gonginxlog access.log

# только 5xx с одного IP за последний час, как совпавшие строки
./gonginxlog --ip 203.0.113.5 --status 5xx --last 1h --lines --report=false access.log

# формат log_format берём из реального nginx.conf (include учитываются)
./gonginxlog --nginx-conf /etc/nginx/nginx.conf --format-name main_json access.log

# тайлинг живого файла (только сырые строки)
./gonginxlog -f /var/log/nginx/access.log

# живой дашборд в стиле k9s
./gonginxlog --ui /var/log/nginx/access.log
```

Флаги можно указывать до или после путей к файлам — `gonginxlog
access.log --top 5` и `gonginxlog --top 5 access.log` работают одинаково.

`gonginxlog --version` печатает версию/коммит/дату сборки. Сообщения
`error:` и `warning:` подсвечиваются цветом (красный/жёлтый), если
stderr — терминал; `NO_COLOR=1` отключает подсветку.

## Примеры на каждый день

```sh
# сколько всего 5xx и на каких путях они возникают (batch-отчёт покажет топ путей)
./gonginxlog --status 5xx access.log

# конкретный клиент подозрительно активен — что он делал за последние 10 минут
./gonginxlog --ip 198.51.100.77 --last 10m --lines --report=false access.log

# перебор /login с одного IP — сколько попыток и с каким UA
./gonginxlog --ip 198.51.100.77 --field request='POST /login' access.log

# весь НЕуспешный трафик (не 2xx) из одной страны — прямого "исключить" нет,
# но можно перечислить остальные классы явно
./gonginxlog --country RU --status 1xx,3xx,4xx,5xx access.log

# найти ботов по User-Agent
./gonginxlog --field http_user_agent='(?i)bot|crawler|spider' --lines --report=false access.log

# конкретный referer (например, откуда идёт трафик на промо-страницу)
./gonginxlog --field http_referer='partner-site.example.com' access.log

# только запросы с аномально долгим request_time (через grep по сырой JSON-строке)
./gonginxlog --grep '"request_time":"([1-9][0-9]|[0-9]{3,})\.' access.log

# несколько ротированных логов и gzip-архивы сразу
./gonginxlog access.log access.log.1 access.log.2.gz --top 20

# лог с удалённого сервера через ssh, без копирования на диск
ssh prod-fe03 'cat /var/log/nginx/access.log' | ./gonginxlog -

# отчёт как JSON — для дальнейшей обработки через jq
./gonginxlog --json access.log | jq '.TopIPs[0:5]'

# отчёт как JSON — топ стран с процентом от общего числа запросов
./gonginxlog --json access.log | jq '.TotalRequests as $t | .TopCountries | map({country: .Key, pct: (((.Count/$t*1000)|round)/10)})'

# сравнить трафик за два конкретных получаса (--since/--until нужна полная дата, не только время)
./gonginxlog --since 2026-07-22T15:00:00+03:00 --until 2026-07-22T15:30:00+03:00 access.log
./gonginxlog --since 2026-07-22T15:30:00+03:00 --until 2026-07-22T16:00:00+03:00 access.log

# полчаса детализации по временной гистограмме вместо авто-подбора бакета
./gonginxlog --bucket 30m access.log

# конфиг с несколькими log_format — явно выбираем нужный
./gonginxlog --nginx-conf /etc/nginx/nginx.conf --format-name json_combined access.log

# тайлинг конкретного виртуального хоста в реальном времени
./gonginxlog -f /var/log/nginx/access.log | grep --line-buffered 'example.com'
```

## Ввод данных

Позиционные аргументы — один или несколько файлов лога, читаются и
объединяются по порядку:

```sh
gonginxlog access.log access.log.1 access.log.2.gz   # несколько файлов, gzip распаковывается прозрачно
cat access.log | gonginxlog -                          # "-" читает stdin
gonginxlog -f access.log                                # режим tail -f
```

`-f`/`--follow` принимает ровно один реальный файл (не `-`, не несколько
файлов) и стримит новые совпавшие строки. Пересоздаёт файл, если он
"похудел" (ротация через `copytruncate` или rename+recreate). Никакой
статистики не считает — для статистики, живой или нет, используйте
`--ui` (см. [Live TUI](#live-tui-дашборд---ui) ниже) или batch-отчёт.

## Выбор log_format

По умолчанию gonginxlog использует встроенный формат, эквивалентный:

```
log_format main_json escape=json '{'
        '"remote_addr":"$remote_addr", ... '"x-request-id":"$request_id"'
'}';
```

Чтобы использовать свой реальный nginx-конфиг:

```sh
gonginxlog --nginx-conf /etc/nginx/nginx.conf --format-name main_json access.log
```

- `--nginx-conf` читается вместе со всем, что он `include`-ит (глобы и
  рекурсия) — так что `log_format`, объявленный в `conf.d/*.conf` или
  `sites-enabled/*`, тоже находится.
- `--format-name` выбирает, какую директиву `log_format <имя> ...;`
  использовать, если в конфиге их несколько.
- Поддерживаются и `escape=json` форматы (парсятся как настоящий JSON,
  порядок ключей не важен), и обычные текстовые форматы (в духе
  `combined`, компилируются в regex с именованными группами из
  `$variable`-токенов).
- Поля сопоставляются внутри по имени nginx-**переменной**, а не по
  тому, какой JSON-ключ вы выбрали — `"x-request-id":"$request_id"`
  всё равно распознаётся как `request_id`. Именно это позволяет
  фильтрам и отчёту работать независимо от кастомных имён JSON-ключей.
- Если `--nginx-conf` указан, но нужный `log_format` в нём (или во всех
  его include) не найден — gonginxlog завершится с ошибкой, а не тихо
  подставит встроенный дефолт. Укажите правильный `--format-name`.

## Фильтры

| Флаг | Что делает | Примеры |
|---|---|---|
| `--since` | оставить записи от этого момента и позже | `--since 2026-08-12T10:00:00Z`, `--since "12/Aug/2026:10:00:00 +0000"` |
| `--until` | оставить записи строго до этого момента | `--until 2026-08-12T12:00:00Z` |
| `--last` | записи за последний интервал (перекрывает `--since`) | `--last 1h30m` |
| `--status` | фильтр по статусу: точный код / класс / диапазон, через запятую | `--status 404,500-599,3xx` |
| `--ip` | фильтр по IP клиента: точный / CIDR, через запятую | `--ip 203.0.113.5,10.0.0.0/8` |
| `--country` | фильтр по коду страны, через запятую (см. ниже) | `--country RU,US` |
| `--grep` | regex по всей сырой строке лога | `--grep 'wp-admin'` |
| `--field` | regex по одному полю, можно повторять (объединяются через AND) | `--field http_user_agent=bot` `--field host=example\.com` |

Имена полей для `--field` — это имена nginx-переменных (`remote_addr`,
`status`, `request`, `http_user_agent`, `http_referer`, `host`, ...) —
те же имена, что использует отчёт и все остальные фильтры внутри.

`--since`/`--until` принимают RFC3339 либо собственный формат nginx
`time_local` (`02/Jan/2006:15:04:05 -0700`).

`--country` матчится по `$geoip_country_code` (старый
`ngx_http_geoip_module`) или `$geoip2_data_country_code`
(`ngx_http_geoip2_module`) — что найдётся в вашем log_format. Работает
(и таблица "Top countries" в отчёте появляется) только если один из
этих полей реально есть в формате.

## Вывод

По умолчанию gonginxlog печатает агрегированный отчёт:

- распределение по кодам статуса
- топ клиентских IP, стран, запрошенных путей, User-Agent, referer'ов
  (`--top N` строк для каждого, по умолчанию 10; таблица стран
  появляется только если в log_format есть `$geoip_country_code` или
  `$geoip2_data_country_code` — запросы без определённой страны
  считаются в строке `-`)
- перцентили request_time / upstream_response_time (avg, p50, p90, p99, max)
- суммарный объём переданных байт
- гистограмма запросов по времени (`--bucket`; по умолчанию `auto` сам
  подбирает размер бакета — 1m/5m/15m/30m/1h/3h/6h/12h/24h — по
  реальному временному диапазону данных, чтобы график оставался
  читаемым; можно задать явно, например `--bucket 30m`, или отключить
  через `--bucket 0`)

Флаги, управляющие тем, что печатается:

```sh
gonginxlog access.log                       # только отчёт (по умолчанию)
gonginxlog --lines access.log               # отчёт + совпавшие сырые строки
gonginxlog --lines --report=false access.log  # только совпавшие строки, как grep
gonginxlog --json access.log                # отчёт в виде JSON вместо текстовых таблиц
```

## Live TUI-дашборд (`--ui`)

```sh
gonginxlog --ui /var/log/nginx/access.log

# стартовые фильтры продолжают действовать и внутри дашборда
gonginxlog --status 5xx --ui --ui-buffer 20000 /var/log/nginx/access.log

# сразу открыть дашборд только по одной стране
gonginxlog --country RU --ui /var/log/nginx/access.log
```

Дашборд в стиле k9s: одна полноэкранная таблица за раз, переключение по
хоткею, обновление раз в секунду. Требует ровно один реальный файл (как
и `-f`). При запуске сначала читает уже существующее содержимое файла
батчем (чтобы дашборд не был пустым), затем переходит в живой тайлинг.

Хоткеи: `1` status · `2` ips · `3` countries (только если в log_format
есть geoip) · `4` paths · `5` agents · `6` referers · `7` timeline ·
`l` raw (живые совпавшие строки) · `a` alerts · `/` фильтр · `x` сброс
фильтра · `Enter` drill-down по выбранной строке · `Esc` назад/закрыть ·
`q` выход · `j`/`k` работают как стрелки вниз/вверх.

**Фильтр внутри дашборда (`/`)** использует тот же мини-язык везде,
токены через пробел объединяются через AND: `status:5xx`,
`ip:203.0.113.5`, `country:RU,US`, `grep:<regex>` (по всей строке), или
`<поле>=<regex>` (одно поле, как `--field`). Накладывается поверх того,
что было передано флагами при старте. Применение или сброс (`x`)
пересканирует файл, поэтому на очень большом логе может занять момент.

Примеры фильтров прямо в дашборде:

```
/status:404,500-599
/ip:66.181.42.0/24
/country:RU grep:wp-admin
/http_user_agent=(?i)bot
```

**Drill-down (`Enter`)** на любой строке показывает разбивку по кодам
статуса и топ связанных путей/IP для этого IP/пути/страны/статуса/
UA/referer'а, посчитанные по последним `--ui-buffer` запросам (по
умолчанию 10000) — не по всей истории файла, поэтому на долгой сессии
очень старая активность может "выпасть" из drill-down, хотя в общих
счётчиках она всё ещё учтена.

**Alerts (`a`)** показывает аномалии от трёх детекторов с фиксированными
порогами: один IP генерирует ≥30% трафика за последние 60с (флуд), один
IP касается ≥20 разных путей за последние 10с (скан), один путь
получает запросы от ≥15 разных IP за последние 10с (распределённый
перебор). В хедере показывается бейдж `⚠ N alert(s)`, когда есть
активные алерты.

## Docker

Маленький (~6МБ) статический образ публикуется при каждом push в `main`
и на каждый тег версии, в оба реестра. `--ui` нужен настоящий терминал
(`docker run -it`):

```sh
docker pull ghcr.io/north21/gonginxlog:latest
docker pull <dockerhub-user>/gonginxlog:latest
```

Запуск на лог с хоста через volume:

```sh
docker run --rm -v /var/log/nginx:/logs:ro \
  ghcr.io/north21/gonginxlog:latest --status 5xx /logs/access.log

# live-дашборд в контейнере (нужен -it для терминала)
docker run --rm -it -v /var/log/nginx:/logs:ro \
  ghcr.io/north21/gonginxlog:latest --ui /logs/access.log
```

Теги: `latest` (следует за `main`), `vX.Y.Z` / `X.Y` (из релизных
тегов), и `sha-<short-sha>`.

## Релизы

Каждый тег `vX.Y.Z` также собирает автономные бинарники
(linux/darwin/windows, amd64/arm64) и публикует их как GitHub Release, с
`checksums.txt`. Скачайте нужный со страницы Releases и запускайте — не
нужен ни Go, ни Docker.

```sh
curl -LO https://github.com/north21/gonginxlog/releases/download/v0.2.0/gonginxlog_0.2.0_linux_amd64.tar.gz
tar -xzf gonginxlog_0.2.0_linux_amd64.tar.gz
./gonginxlog --version
```

## Roadmap

Пока не реализовано:

- Тайлинг нескольких файлов в `--ui` (сейчас ровно один файл, как `-f`).
- Настраиваемые пороги для детекции аномалий (сейчас фиксированные, см. выше).
- Интерактивный просмотр в TUI статичного (не live) файла.

Подробное обоснование архитектурных решений и структура пакетов — в
`DESIGN.md` (на английском, для контрибьюторов). Краткая ориентация для
Claude Code при работе в этом репозитории — в `CLAUDE.md`.
