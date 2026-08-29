# Gateway API

OpenSwiftScale exposes an OpenAI-compatible inference API for applications inside your network. The API is separate from the administrator console: clients use a Gateway API key, while `/api/admin/*` uses the administrator account.

## Base URL

The standard installer detects the gateway host's active LAN address. The resulting client-reachable address is similar to:

```text
http://192.168.1.25:8080/v1
```

Use the console's **Overview → Gateway API** card to copy the address of the current instance. `127.0.0.1` and `localhost` are reachable only from the gateway host and are not shown as the internal API address. Production environments should normally configure the private DNS name or internal load-balancer address that exposes OpenSwiftScale, for example:

```text
https://ai-gateway.internal.example.com/v1
```

Terminate TLS at a trusted internal reverse proxy or ingress when requests cross a host boundary.

## Authentication

Every inference request must include a Gateway API key as a Bearer token:

```http
Authorization: Bearer <gateway-api-key>
```

Create normal client credentials in **Console → API access**. Create one API user for each person, application, team, or environment, then issue one or more independently revocable keys to that user. The complete key is shown once; OpenSwiftScale stores only its SHA-256 hash. Disabling a user immediately blocks all of that user's keys, while revoking one key leaves the user's other keys active. The console also records each key's creation time and last-used time.

`scripts/install.sh` generates an initial bootstrap key in `secrets/gateway_api_keys.txt`. The server reads bootstrap keys from `OPENSWIFTSCALE_API_KEYS` or `OPENSWIFTSCALE_API_KEYS_FILE` for first-run and compatibility purposes. Prefer managed keys from **API access** for applications and regular users. Do not use the console administrator password as an inference key, commit keys to source control, or expose them in browser-delivered application code.

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/v1/models` | List models that currently have an available provider route. |
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat completions, including SSE streaming. |
| `POST` | `/v1/responses` | OpenAI Responses-compatible requests for supported providers. |
| `POST` | `/v1/embeddings` | Embeddings for routes that advertise embedding capability. |

Health endpoints do not use inference authentication: `GET /healthz` reports process health and `GET /readyz` reports readiness. Administrator endpoints under `/api/admin/*` are intended for the embedded console and use separate HTTP Basic authentication. API-user and key management is available through `/api/admin/users`; it never returns a previously created plaintext key.

## Discover available models

```bash
export OPEN_SWIFT_SCALE_KEY="<key-created-in-api-access>"

curl http://192.168.1.25:8080/v1/models \
  -H "Authorization: Bearer $OPEN_SWIFT_SCALE_KEY"
```

Only models with an enabled connection and a saved Provider API key are returned. Use the returned model `id` exactly in inference requests; it selects the matching route rule.

## Chat completions

```bash
curl http://192.168.1.25:8080/v1/chat/completions \
  -H "Authorization: Bearer $OPEN_SWIFT_SCALE_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.6-luna",
    "messages": [
      {"role": "user", "content": "Explain this architecture."}
    ],
    "stream": false
  }'
```

Set `stream` to `true` to receive upstream Server-Sent Events. OpenSwiftScale streams the provider response without persisting prompt bodies.

## Client examples

The Models workspace shows which exact model IDs currently have a ready endpoint. Use one of those IDs with the cURL, Python, Go, Node.js, Java, or Rust examples below.

### Python OpenAI SDK

Python:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://192.168.1.25:8080/v1",
    api_key="<gateway-api-key>",
)

response = client.chat.completions.create(
    model="gpt-5.6-luna",
    messages=[{"role": "user", "content": "Hello"}],
)
print(response.choices[0].message.content)
```

### Node.js OpenAI SDK

```typescript
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "http://192.168.1.25:8080/v1",
  apiKey: "<gateway-api-key>",
});

const response = await client.chat.completions.create({
  model: "gpt-5.6-luna",
  messages: [{ role: "user", content: "Hello" }],
});
console.log(response.choices[0].message.content);
```

### Go standard library

```go
import (
    "bytes"
    "fmt"
    "io"
    "net/http"
)

body := []byte(`{"model":"gpt-5.6-luna","messages":[{"role":"user","content":"Hello"}]}`)
req, _ := http.NewRequest("POST", "http://192.168.1.25:8080/v1/chat/completions", bytes.NewReader(body))
req.Header.Set("Authorization", "Bearer <gateway-api-key>")
req.Header.Set("Content-Type", "application/json")
response, err := http.DefaultClient.Do(req)
if err != nil {
    panic(err)
}
defer response.Body.Close()
output, _ := io.ReadAll(response.Body)
fmt.Println(string(output))
```

### Java HTTP client

```java
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;

var request = HttpRequest.newBuilder(
        URI.create("http://192.168.1.25:8080/v1/chat/completions"))
    .header("Authorization", "Bearer <gateway-api-key>")
    .header("Content-Type", "application/json")
    .POST(HttpRequest.BodyPublishers.ofString(
        "{\"model\":\"gpt-5.6-luna\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}]}"))
    .build();
var response = HttpClient.newHttpClient()
    .send(request, HttpResponse.BodyHandlers.ofString());
System.out.println(response.body());
```

### Rust with reqwest

```rust
let response = reqwest::Client::new()
    .post("http://192.168.1.25:8080/v1/chat/completions")
    .bearer_auth("<gateway-api-key>")
    .json(&serde_json::json!({
        "model": "gpt-5.6-luna",
        "messages": [{"role": "user", "content": "Hello"}]
    }))
    .send().await?;
println!("{}", response.text().await?);
```

Use the console-generated examples when possible: they automatically use the current `OPENSWIFTSCALE_PUBLIC_URL` and selected callable model ID.

## Routing behavior

The request's `model` value is the Public Model ID. OpenSwiftScale resolves its configured endpoint pool as follows:

1. The request selects one exact model ID.
2. Endpoints capable of serving that exact model are inspected in their visible order.
3. Connection failures, timeouts, and retryable upstream responses advance to another eligible endpoint for the same model.
4. Ordinary model requests are never silently replaced with a different model ID.

Successful inference responses identify the selected route with these headers:

- `X-OpenSwiftScale-Provider`
- `X-OpenSwiftScale-Route-Priority`

See [Configuration](CONFIGURATION.md#same-model-endpoint-routing) for endpoint ordering and failover details. Cross-model alias headers may still appear for installations carrying deprecated compatibility rules; new configurations should not depend on them.

## Errors and request limits

Authentication failures return HTTP `401` with an `invalid_api_key` error. Unknown or unavailable model IDs and invalid request bodies return a JSON error response. Per-key request and concurrency limits are controlled by `OPENSWIFTSCALE_RATE_LIMIT_RPM` and `OPENSWIFTSCALE_CONCURRENCY`; exceeding a limit returns HTTP `429`.

Provider errors that are not eligible for failover are passed back to the client. Keep the response request ID and OpenSwiftScale route headers when troubleshooting.

## Production checklist

- Publish the gateway through a private DNS name and TLS-enabled internal ingress.
- Keep inference keys separate from administrator credentials and provider keys.
- Give different applications different Gateway API keys so they can be rotated independently.
- Restrict network access to approved workloads and never expose the default local deployment directly to the public Internet.
- Verify provider model IDs, capabilities, pricing, and data-handling terms before production use.
