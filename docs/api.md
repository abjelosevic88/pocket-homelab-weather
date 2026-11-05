# API

Everything is under `/api/v1`, JSON in and out, with a session cookie, a personal token (`Authorization: Bearer pwx_…`, Settings → API & shortcuts) or a Pocket Core link token. Data is metric: °C, mm of rain, cm of snow, km/h. Times are the local wall clock of the place: `2026-11-06T07:00`.

## Reading

| | |
|---|---|
| `GET /summary[?location=ID]` | home now, today, tomorrow, its warnings and a one-line `text` (Homepage, Pocket Day, Pocket AI); `{"place": null}` before the first place |
| `GET /overview` | every place with now, today and tomorrow, and all active warnings |
| `GET /forecast?location=ID` or `?lat=&lon=` `[&hours=48&air_hours=1]` | now, the hours from now (up to 240), yesterday + ten days, air and pollen per day, the warnings |
| `GET /air?location=ID` or `?lat=&lon=` | European AQI now, per day and per hour, with pollen (Europe only) |
| `GET /warnings[?history=1&location=ID&fresh=1]` | active warnings (`level` is `advisory` = heads-up, or `warning`), and the last 30 days |
| `GET /day/YYYY-MM-DD[?location=ID or ?lat=&lon=]` | one day: from the diary (with your `note`), the forecast (up to ten days ahead) or the archive — what Pocket Day reads |
| `GET /diary?location=ID&month=YYYY-MM` | a month of the diary with its numbers, and last year's same days |
| `GET /locations`, `GET /locations/{id}` | your places |
| `GET /search?q=Mostar` | find places by name |
| `GET /reverse?lat=&lon=` | name a spot |
| `GET /plans[?fresh=1]` | your next days' events with their weather, your trips, and advice from the linked apps (plants, tyres, dog walks) |

## Changing

| | |
|---|---|
| `POST /locations` | `{"name":"Vienna","lat":48.2085,"lon":16.3721,"country":"Austria","is_home":false}`; another app may send `source_ref` to add the same place only once |
| `PUT /locations/{id}` | `name`, `note`, `is_home`, `warn` (warnings and the diary for it) |
| `DELETE /locations/{id}` | with its diary and warnings |
| `POST /locations/order` | `{"ids":[3,1,2]}` |
| `PUT /diary/{location}/{date}` | `{"note":"First snow"}` |
| `POST /check` | check the warnings now |
| `POST /advice/mark` | `{"key":"tyres:winter:2026","state":"done"}` (`hidden`, or `""` to undo) |
| `POST /advice/task` | `{"key":"…"}` — make it a task in Pocket Tasks |
| `POST /ai/brief` | `{"location_id":1}` → the day in plain words (kept three hours) |
| `POST /ai/ask` | `{"question":"Can I hang the washing out tomorrow?"}` |
| `POST /notify/check` | send the morning message now |
| `POST /import` | a Pocket Weather export (JSON) |

`GET /export.json` and `GET /export.csv` (the diary) download your data; `GET /backup` (admins) the whole database.

## Pocket Core

Link tokens: `weather:read` covers the reads above except `/plans` (it carries what other apps told Weather); `weather:write` covers `POST /locations` and `PUT`/`DELETE /locations/{id}` of places that app added.

Events on the Core bus: `weather.warning` (a new or worse warning: `kind`, `level`, `day`, `starts_at`, `ends_at`, `title`, `detail`, `place`), `weather.today` (the morning: `place`, `text`, `t_min`, `t_max`, `condition`, `warnings`).
