# wishlist

Онлайн-вишлист с анонимным бронированием подарков. Один бинарник на Go без внешних зависимостей, данные в одном JSON-файле, работает в Docker на Raspberry Pi.

- Админка: вставляешь ссылку на товар из любого магазина, название, картинка и цена подтягиваются со страницы (OpenGraph, JSON-LD). Всё можно поправить руками.
- Гости видят карточки с превью, ценой и ссылкой на магазин. «Подарю это» бронирует подарок без регистрации, и карточка сереет у всех остальных.
- После брони гость видит адрес доставки: США или Канада. Адрес выбирается по магазину (amazon.com → США, amazon.ca → Канада) или, для прочих магазинов, по стране посетителя из заголовка Cloudflare `CF-IPCountry`. Гость может переключить адрес.
- Управление бронью по секретной ссылке: «я купил» или «отменить». Браузер помнит свои брони сам.
- Бронь истекает через N дней (14 по умолчанию), если её не отметили как купленную.
- Режим без спойлеров: админ по умолчанию не видит, что забронировано.
- Уведомления владельцу письмом о бронях, покупках, отменах и истечениях (на адрес отправителя или `NOTIFY_EMAIL`).
- Email гостю (необязательно): ссылка на бронь и напоминание за 3 дня до окончания.
- Количество (например «хочу 3 штуки»), заметки к товару, приоритет «очень хочу», статусы «получен» и «уже есть».
- RU/EN по языку браузера, с переключателем. `noindex` для поисковиков. Ограничение частоты бронирований и попыток входа.

## Как устроено

```
Mac: правка кода → git push ─▶ GitHub (этот репозиторий, CI: vet + тесты)
                                 │
Pi:  docker compose build ◀──────┘  (образ собирается прямо из GitHub)
     docker compose up -d
```

На хосте лежат только `docker-compose.yml` и `.env`.

| Что | Имя |
|---|---|
| Проект Compose | `wishlist` |
| Контейнер | `wishlist` |
| Сеть | `wishlist_net` |
| Том | `wishlist_data` (`wishlist.json`, `session.key`, `images/`) |
| Порт | `8095` |

Адреса доставки хранятся в `wishlist.json` в томе и задаются в админке, в разделе «Настройки». В репозитории их нет.

## Установка

```bash
mkdir -p ~/wishlist && cd ~/wishlist
curl -fsSLO https://raw.githubusercontent.com/WhyElEss/wishlist/main/docker-compose.yml
curl -fsSL -o .env https://raw.githubusercontent.com/WhyElEss/wishlist/main/.env.example
chmod 600 .env
docker compose build
docker compose run --rm -it wishlist hashpw
```

Строку `ADMIN_PASSWORD_HASH=...`, которую выведет последняя команда, впишите в `.env`. Затем:

```bash
docker compose up -d
```

Сайт откроется на `http://<хост>:8095`, админка на `/admin`.

## Обновление

```bash
cd ~/wishlist && docker compose build --no-cache && docker compose up -d
```

## Резервная копия

```bash
docker run --rm -v wishlist_data:/data -v "$PWD":/backup alpine tar czf /backup/wishlist-data.tgz -C /data .
```

## Переменные окружения

См. [.env.example](.env.example).
