# Дорожный набор для Docker-демонстрации

`ekaterinburg.osm.gz` — сжатый исходный XML дорог Екатеринбурга, 7,1 MB.
Источник: © OpenStreetMap contributors, https://www.openstreetmap.org/copyright
Лицензия базы данных: ODbL 1.0, https://opendatacommons.org/licenses/odbl/1-0/
Выгружен 14 ноября 2026 через https://overpass-api.de/api/interpreter.
Границы (юг, запад, север, восток): 56.65,60.35,57.05,60.95.
Запрос: все highway ways и restriction relations, затем рекурсивно их узлы.
Исходный XML: 45 059 603 байта. Маршрутизатор OSRM готовит граф внутри
Docker-тома при первом запуске. При повторном запуске использует готовый граф.
Наличие файла избавляет первый запуск от зависимости от Overpass.
