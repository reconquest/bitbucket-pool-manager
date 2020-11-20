# Bitbucket-pool-manager

**Bitbucket-pool-manager** is a tool for automatic creation bitbucket instances,
with automatic setting login/password, URL, installing the addon,
setting the license for the addon.

This tool creates a configurated initial container, updates the status of a container
on "allocated" if this container has already been requested by someone.
Also program automatically remove containers, after a duration, configured in constants.
The program supported API requests for creating bitbucket instance or removing,
receiving free container, receiving data of container by id in JSON.

## Configuration

**Bitbucket-pool-manager** must be configured before using, the configuration
file should be placed in `config.yaml` and must be written using the following syntax:

```yaml
prefix: bitbucket-tests
base_url: /api/v1/bitbucket/servers
listening_port: :10000
bitbucket:
    url: bitbucket.local
    username: admin
    password: admin
    version: 6.8.0
    jvm_support_recommended_args:
    server_proxy_name: bitbucket.local
    elastic_search_enabled: false
database:
    uri: your URI
    name: your database name
```