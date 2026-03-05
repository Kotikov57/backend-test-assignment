Запуск 

docker compose up build


API будет доступен на `http://localhost:8080`, токен: `test-token`.

Пример создания withdrawal:


curl -i http://localhost:8080/v1/withdrawals \
  -H 'Authorization: Bearer test-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "user_id": 1,
    "amount": "10.500000",
    "currency": "USDT",
    "destination": "TXYZ-wallet",
    "idempotency_key": "withdraw-1"
  }'


Пример чтения:

curl -i http://localhost:8080/v1/withdrawals/id \
  -H 'Authorization: Bearer test-token'

Или в Postman, я делал в нём

Тесты

Для тестов нужно запустить make test или make test-cover

Ключевые решения

- Идемпотентность хранится в таблице `idempotency_keys` с уникальным индексом `(user_id, idempotency_key)`.
- На повторный запрос с тем же ключом и тем же payload возвращается уже сохраненный результат.
- На повторный запрос с тем же ключом и другим payload возвращается `422`.
- Баланс блокируется через `SELECT ... FOR UPDATE` внутри транзакции, поэтому конкурентные списания на один и тот же баланс сериализуются и двойное списание не происходит.
- Внутренние ошибки логируются, но наружу возвращается только `internal server error`.
- Денежные суммы обрабатываются в `amount_micros` (`BIGINT`), чтобы не использовать `float`.
