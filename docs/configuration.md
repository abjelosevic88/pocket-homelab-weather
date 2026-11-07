# Configuration

Environment variables; every one has a default. Personal settings (units, warnings, notifications, connections, AI) live in the UI per person — the `OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (links in pushes, the OIDC callback) |
| `DATA_DIR` | `./data` | Database, Core token, backups |
| `DB_PATH` | `$DATA_DIR/pocket-weather.db` | SQLite file |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request, e.g. an export being imported (64 MB) |
| `TZ` | – | Time zone of the box: the morning message and quiet hours (each place has its own time zone for its forecast) |
| `USER_AGENT` | `PocketWeather/1.0 (+github…)` | Sent to Open-Meteo and OpenStreetMap Nominatim |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `NOTIFY_HOUR` | `8` | Default hour of the daily message (each person can change theirs) |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | The day in plain words and questions about the forecast without Pocket AI |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `DEMO_DATA` | `false` | Seed three places around Sarajevo and fourteen months of diary into the owner account, and use made-up weather (a frosty morning, a rainy afternoon, smoggy evenings) and made-up linked apps instead of Open-Meteo and Pocket Core — for screenshots and trying it out, never for real use |

Commands: `pocket-weather backup [-o FILE]`, `pocket-weather reset-password [-email] [-password]`, `pocket-weather version`.

Forecasts come from [Open-Meteo](https://open-meteo.com) (free, no key, CC BY 4.0) and are cached by spot: 20 minutes for the forecast, an hour for air quality and pollen. Place names for "where am I" come from OpenStreetMap Nominatim.

Connections to Pocket Calendar, Travel, Home, Garage, Pets, League, Trails, Movement and Tasks are made per person under Settings → Connected apps: through Pocket Core (nothing to paste) or directly with the app's URL and an API token.
