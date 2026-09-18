# AnyOrigin

A highly customizable CORS proxy built from scratch in Go.

## Features

- **Full CORS Support**: Handles all CORS preflight requests and headers
- **Highly Configurable**: JSON configuration file support
- **Rate Limiting**: Built-in rate limiting per IP address
- **Request Logging**: Optional request logging
- **Timeout Control**: Configurable request timeouts
- **Custom Headers**: Configure allowed/exposed headers
- **Path Prefix Stripping**: Optional path prefix removal for cleaner URLs
- **Credentials Support**: Optional CORS credentials support
- **No External Dependencies**: Pure Go implementation

## Installation

```bash
go build -o anyorigin .
```

## Usage

### Basic Usage (Default Port 8080)

```bash
./anyorigin
```

### Custom Port

```bash
./anyorigin -port 3000
```

### With Configuration File

```bash
./anyorigin -config config.json
```

## Configuration

Create a `config.json` file:

```json
{
  "port": 8080,
  "allowed_origins": ["*"],
  "allowed_methods": ["GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"],
  "allowed_headers": ["*"],
  "exposed_headers": [],
  "max_age": 86400,
  "allow_credentials": false,
  "timeout": 30000000000,
  "strip_path_prefix": "",
  "log_requests": true,
  "rate_limit_enabled": false,
  "rate_limit_requests": 100,
  "rate_limit_window": 60000000000
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `port` | int | 8080 | Port to listen on |
| `allowed_origins` | []string | ["*"] | List of allowed origins |
| `allowed_methods` | []string | All common methods | Allowed HTTP methods |
| `allowed_headers` | []string | ["*"] | Allowed request headers |
| `exposed_headers` | []string | [] | Headers exposed to browser |
| `max_age` | int | 86400 | CORS preflight cache duration (seconds) |
| `allow_credentials` | bool | false | Allow credentials in CORS requests |
| `timeout` | int (nanoseconds) | 30s | Request timeout |
| `strip_path_prefix` | string | "" | Path prefix to strip from requests |
| `log_requests` | bool | true | Enable request logging |
| `rate_limit_enabled` | bool | false | Enable rate limiting |
| `rate_limit_requests` | int | 100 | Max requests per window |
| `rate_limit_window` | int (nanoseconds) | 1m | Rate limit time window |

## API Usage

To proxy a request, append the target URL to the proxy path:

```
GET /https://example.com/api/data
```

Or with URL-encoded target:

```
GET /https%3A%2F%2Fexample.com%2Fapi%2Fdata
```

### Example with curl

```bash
# Simple GET request
curl http://localhost:8080/https://api.example.com/data

# POST request
curl -X POST -H "Content-Type: application/json" \
  -d '{"key":"value"}' \
  http://localhost:8080/https://api.example.com/endpoint

# With custom origin header
curl -H "Origin: https://myapp.com" \
  http://localhost:8080/https://api.example.com/data
```

### Example from Browser

```javascript
fetch('http://localhost:8080/https://api.example.com/data')
  .then(response => response.json())
  .then(data => console.log(data));
```

## Security Considerations

- **Origin Validation**: Configure `allowed_origins` to restrict which origins can use the proxy
- **Rate Limiting**: Enable rate limiting to prevent abuse
- **Timeout**: Set appropriate timeouts to prevent resource exhaustion
- **HTTPS**: Always use HTTPS in production deployments

## Building from Source

```bash
git clone <repository>
cd anyorigin
go build -o anyorigin .
```

## License

MIT License
