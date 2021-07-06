# Redis REST Gateway

# Usage

## Create

```bash
curl --data '{"key": "tomato", "value": "berry"}' -v http://127.0.0.1:8080/create | jq 
```

```json
{
  "uid": 362525195484790800,
  "key": "tomato",
  "value": "berry"
}
```

```
127.0.0.1:6379[4]> GET tomato
"berry"
```

## Read

```bash
curl --data '{"key": "tomato"}' -v http://127.0.0.1:8080/read | jq 
```

```json
{
  "uid": 362525251168370700,
  "key": "tomato",
  "value": "berry"
}
```

## Update

```bash
curl --data '{"key": "tomato", "value": "vegetable"}' -v http://127.0.0.1:8080/update | jq 
```

```json
{
  "uid": 362525312656867300,
  "key": "tomato",
  "value": "vegetable"
}
```

```
127.0.0.1:6379[4]> GET tomato
"vegetable"
```

## Delete

```bash
curl --data '{"key": "tomato"}' -v http://127.0.0.1:8080/delete | jq 
```

```json
{
  "uid": 362525346865610750,
  "key": "tomato"
}
```

```
127.0.0.1:6379[4]> GET tomato
(nil)
```

# Provisioning

## Docker/PodMan

```bash
podman run \
  --network host \
  --interactive \
  --tty \
  --detached \
  --name redis-rest-gateway \
  -e "REDIS_PASSWORD=optional-redis-password" \
  "quay.io/reinventedstuff/redis-rest-gateway:1.0.0" \
  -bind 127.0.0.1:8080 \
  -redis-address 127.0.0.1:6379 \
  -redis-db 0
```
