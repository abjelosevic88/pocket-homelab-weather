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
| `GET /plans[?fresh=1]` | your next days' events with their weather, your coming games (Pocket League) at their venue, your planned outings (Pocket Trails) where the route starts, your trips, and advice from the linked apps (plants, tyres, dog walks) |

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

Pocket Move can copy your places and weather diary out and bring them in: `move:export` reads `GET /export.json` and `GET /export.csv`, `move:import` posts `POST /import` (adds places and days you do not have yet; your home and your own notes stay as they are; a second run adds nothing). `GET /pocket/portable` describes both.

Events on the Core bus: `weather.warning` (a new or worse warning: `kind`, `level`, `day`, `starts_at`, `ends_at`, `title`, `detail`, `place`), `weather.today` (the morning: `place`, `text`, `t_min`, `t_max`, `condition`, `warnings`).

## Pocket Search, Pocket Day and Pocket Hub

Weather answers the four routes every Pocket app can offer (`docs/capabilities.md` in Pocket Core), so Pocket Search, Pocket Day and Pocket Hub find it on their own. Each has its own link scope that opens that one route and nothing else (`weather:read` and `weather:write` do not open them); a signed-in person or a personal token can call them too. None of them fetches from Open-Meteo: they read the forecast Weather keeps for each place (renewed by the background check every 30 minutes for the places with warnings, and whenever a page is open), the saved warnings and the diary. A kept forecast past its 20 minutes is still used, with the time it is from; after an hour, "now" is the kept forecast for this hour rather than what was measured then. A place without a kept forecast shows no weather. Addresses (`url`) are paths in the app.

| | Scope | |
|---|---|---|
| `GET /pocket/search?q&limit` | `pocket:search` | `{items}`: your saved places whose name, region, country or note has the words, with the weather now in `snippet` ("12°, light rain · updated 14:05"); your warnings of the last 30 days and the ones ahead by what they say ("Sarajevo · Sat 28 Nov · Warning"); the diary days whose note has the words. `id` is `place:3`, `warning:12` or `diary:3:2026-11-26`, `kind` is place, warning or diary; a place opens its Today page, a warning the Warnings page, a note the diary. Not a geocoder: a town you have not saved finds nothing. A `q` without a letter or a digit finds nothing |
| `GET /pocket/day/{date}` | `pocket:day` | `{date, summary, items, metrics}` at your main place: the diary for a past day, else the kept forecast (yesterday to nine days ahead), else a day looked up before with `GET /day`. `summary` is "Sarajevo: Light rain, 8 °C to 14 °C, 6 mm", with "(forecast)" for a day ahead; `metrics` are Low, High, Rain, and Snow or Sunshine. `items` are that day's warnings at any of your places (`kind` warning, `sub` the place and what it means, `time`/`end` when it starts and ends that day) and your diary note (`kind` diary, `note` the whole note). The journal counts them as "notes" |
| `GET /pocket/widget` | `pocket:widget` | `{line, metrics, items, url}`: `line` is now and today at home ("12°, light rain · 8° to 14° today", plus "updated 09:10" when the kept forecast is older than an hour); `items` are the soonest four warnings that are on ("Jahorina · tomorrow 06:00–09:00"), then your other places with their weather now, or the next four hours, three hours apart, when you have one place; `metrics` counts the warnings. Nothing without a place |
| `POST /pocket/capture` | `pocket:capture` | `{"request_id":"…","kind":"diary","text":"First snow","date":"2026-11-27"}` adds the line under the note of that day (today by default) in the diary of your main place, on its own line; what is there stays. The diary keeps the days of places with warnings since they were added, so a day ahead, a day not in it, a home without warnings or a note past 2000 characters is refused with a sentence that says so. Today or yesterday not written yet is written from the kept forecast first, as the check would. The same `request_id` again answers the same and adds nothing; with other words it is refused |

