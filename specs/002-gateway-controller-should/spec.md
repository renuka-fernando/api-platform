# Feature Specification: Gateway Controller Platform API Integration

**Feature Branch**: `002-gateway-controller-should`
**Created**: 2025-10-14
**Status**: Draft
**Input**: User description: "Gateway Controller should connect to Platform API to get API deployment events and fetch APIs via REST API. There should be a config to on/off gateway connection to platform API, if off the gateway operates independently."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Gateway Receives Deployment Notifications in Real-Time (Priority: P1)

When APIs are deployed, updated, or deleted through the Platform API, the Gateway Controller must be notified immediately so that it can synchronize its local configuration without manual intervention or polling delays.

**Why this priority**: This is the core capability that enables the GitOps workflow and distributed API management. Without real-time synchronization, gateways cannot automatically reflect changes made through the control plane.

**Independent Test**: Can be fully tested by deploying an API through Platform API and verifying the Gateway Controller receives the event and updates its configuration within 5 seconds, delivering immediate API availability at the gateway.

**Acceptance Scenarios**:

1. **Given** a Gateway Controller is connected to Platform API, **When** an administrator deploys a new API through Platform API, **Then** the Gateway Controller receives a deployment event notification within 5 seconds
2. **Given** a Gateway Controller has an API configured, **When** that API is updated in Platform API, **Then** the Gateway Controller receives an update event and refreshes the API configuration
3. **Given** a Gateway Controller has an API configured, **When** that API is deleted in Platform API, **Then** the Gateway Controller receives a deletion event and removes the API from its configuration
4. **Given** a Gateway Controller loses connection to Platform API, **When** the connection is restored, **Then** the Gateway Controller re-establishes the event stream and receives any missed events

---

### User Story 2 - Gateway Authenticates Securely with Platform API (Priority: P1)

The Gateway Controller must authenticate itself with the Platform API using a configured credential to ensure only authorized gateways can receive deployment events and fetch API configurations.

**Why this priority**: Security is critical for the control plane communication. Without proper authentication, unauthorized systems could receive sensitive API configurations or inject malicious updates.

**Independent Test**: Can be fully tested by starting a Gateway Controller with valid and invalid credentials, verifying that only valid credentials allow connection and API fetching, delivering secure control plane communication.

**Acceptance Scenarios**:

1. **Given** a Gateway Controller with a valid API key configured, **When** it attempts to connect to Platform API, **Then** the connection succeeds and the Gateway receives deployment events
2. **Given** a Gateway Controller with an invalid API key, **When** it attempts to connect to Platform API, **Then** the connection is rejected with a 401 Unauthorized response
3. **Given** a Gateway Controller with a missing API key, **When** it attempts to connect to Platform API, **Then** the connection is rejected and an error is logged
4. **Given** a connected Gateway Controller, **When** its API key is revoked in Platform API, **Then** the connection is terminated and the Gateway stops receiving events

---

### User Story 3 - Gateway Fetches API Configurations on Demand (Priority: P2)

When the Gateway Controller receives a deployment event, it must be able to fetch the complete API configuration details from the Platform API to apply the changes locally and update the Envoy proxy.

**Why this priority**: While event notifications enable reactivity, the actual API configurations must be retrieved to complete the synchronization. This is a dependent capability on P1 stories.

**Independent Test**: Can be fully tested by triggering a deployment event and verifying the Gateway fetches the full API configuration via REST API, then successfully applies it to Envoy, delivering end-to-end deployment automation.

**Acceptance Scenarios**:

1. **Given** a Gateway Controller receives a deployment event with API identifier, **When** it requests the API configuration from Platform API, **Then** it receives a complete API.yaml specification
2. **Given** a Gateway Controller fetches an API configuration, **When** the configuration is valid, **Then** the Gateway parses and stores it in its local cache and database
3. **Given** a Gateway Controller fetches an API configuration, **When** the configuration is invalid or malformed, **Then** the Gateway logs an error and does not apply the configuration
4. **Given** multiple deployment events arrive rapidly, **When** the Gateway processes them, **Then** each API configuration is fetched and applied in the correct order without race conditions

---

### User Story 4 - Gateway Reconnects Automatically After Connection Failures (Priority: P2)

If the Gateway Controller loses connection to the Platform API due to network issues or Platform API restarts, it must automatically attempt to reconnect with exponential backoff to restore event synchronization.

**Why this priority**: Resilience is important for production deployments, but the core synchronization (P1) must work first. This ensures long-running reliability.

**Independent Test**: Can be fully tested by simulating network failures or Platform API downtime, then verifying the Gateway reconnects automatically and resumes receiving events, delivering fault-tolerant operations.

**Acceptance Scenarios**:

1. **Given** a connected Gateway Controller, **When** the Platform API becomes unreachable, **Then** the Gateway detects the disconnection within 30 seconds
2. **Given** a disconnected Gateway Controller, **When** attempting to reconnect, **Then** it uses exponential backoff (1s, 2s, 4s, 8s, up to 60s maximum)
3. **Given** a Gateway Controller reconnects after a disconnection, **When** the connection is re-established, **Then** it fetches any API configurations that changed during the downtime
4. **Given** a Gateway Controller is in reconnection mode, **When** reconnection attempts continue, **Then** it retries indefinitely using the maximum backoff interval (60 seconds) to ensure eventual consistency when Platform API recovers

---

### User Story 5 - Gateway Operates Independently Without Platform API (Priority: P1)

Administrators must be able to configure the Gateway Controller to run in standalone mode without connecting to Platform API, enabling fully independent deployments where API configurations are managed directly via the Gateway Controller's REST API.

**Why this priority**: This enables flexible deployment models - from fully autonomous edge gateways to hybrid architectures. It's fundamental to supporting both cloud-managed and on-premise deployment modes mentioned in the platform architecture.

**Independent Test**: Can be fully tested by starting Gateway Controller with Platform API integration disabled, deploying APIs via local REST API, and verifying full functionality without any Platform API connection attempts, delivering autonomous gateway operation.

**Acceptance Scenarios**:

1. **Given** a Gateway Controller with Platform API integration disabled in configuration, **When** the Gateway starts up, **Then** no connection attempts are made to Platform API
2. **Given** a Gateway Controller running in standalone mode, **When** an API is deployed via the local REST API, **Then** the API is configured and available without any Platform API interaction
3. **Given** a Gateway Controller with Platform API integration enabled, **When** the Gateway starts up, **Then** it attempts to connect to Platform API and receive deployment events
4. **Given** a Gateway Controller configuration, **When** Platform API integration is toggled from enabled to disabled, **Then** any active connections are closed gracefully and no reconnection attempts occur
5. **Given** a Gateway Controller running in standalone mode, **When** an administrator checks the health endpoint, **Then** the Platform API connection status shows as "disabled" rather than "disconnected"

---

### User Story 6 - Administrators Monitor Gateway Connection Status (Priority: P3)

Operators need visibility into whether the Gateway Controller is successfully connected to the Platform API and receiving events to troubleshoot synchronization issues.

**Why this priority**: Observability is important but not blocking for basic functionality. This enhances operational confidence after core features work.

**Independent Test**: Can be fully tested by checking Gateway Controller logs and health endpoints to verify connection status reporting, delivering operational visibility.

**Acceptance Scenarios**:

1. **Given** a Gateway Controller with Platform API enabled is running, **When** an administrator checks the health endpoint, **Then** the response includes Platform API connection status (connected/disconnected/disabled)
2. **Given** a Gateway Controller connects to Platform API, **When** the connection succeeds, **Then** a log entry is written with timestamp and Platform API URL
3. **Given** a Gateway Controller fails to connect, **When** the connection attempt fails, **Then** an error log is written with the failure reason and retry schedule
4. **Given** a Gateway Controller is receiving events, **When** an event is processed, **Then** a debug-level log entry records the event type and API identifier
5. **Given** a Gateway Controller is running in standalone mode, **When** it starts up, **Then** a log entry confirms Platform API integration is disabled

---

### Edge Cases

- What happens when the Gateway Controller receives a deployment event for an API that no longer exists in Platform API (deleted between event and fetch)?
- How does the system handle duplicate deployment events for the same API arriving in quick succession?
- What happens when the Platform API returns a partial or corrupted API configuration during fetch?
- How does the Gateway behave if the configured Platform API URL is unreachable at startup when integration is enabled?
- What happens when the Gateway Controller's API key is rotated while a connection is active?
- How does the system handle network partitions where the WebSocket connection appears active but no data is transmitted?
- What happens when the Gateway Controller's local storage (bbolt) fails during API configuration persistence after a fetch?
- How does the Gateway behave if Platform API integration is enabled but required configuration (URL or API key) is missing?
- What happens when an administrator changes the Platform API integration setting (enabled/disabled) without restarting the Gateway Controller?
- How does the Gateway handle APIs deployed via local REST API when Platform API integration is later enabled?

## Requirements *(mandatory)*

### Functional Requirements

#### Standalone Mode (Platform API Integration Disabled)

- **FR-001**: Gateway Controller MUST support a configuration flag to enable/disable Platform API integration (default: disabled for backward compatibility)
- **FR-002**: Gateway Controller MUST NOT attempt any Platform API connections when integration is disabled
- **FR-003**: Gateway Controller MUST continue accepting API configurations via its local REST API when running in standalone mode
- **FR-004**: Gateway Controller MUST operate all existing features (API management, xDS, storage) independently when Platform API integration is disabled
- **FR-005**: Gateway Controller MUST indicate "disabled" status for Platform API connection in health endpoint when integration is disabled

#### Connected Mode (Platform API Integration Enabled)

- **FR-006**: Gateway Controller MUST establish a WebSocket connection to Platform API at startup when integration is enabled
- **FR-007**: Gateway Controller MUST include the configured API key as a header in all Platform API requests (both WebSocket and REST)
- **FR-008**: Gateway Controller MUST receive deployment events via WebSocket including API deployment, update, and deletion events
- **FR-009**: Gateway Controller MUST fetch complete API configurations from Platform API via REST API when deployment events are received
- **FR-010**: Gateway Controller MUST parse fetched API configurations and validate them before applying to local storage
- **FR-011**: Gateway Controller MUST update its in-memory cache and persistent storage (bbolt) with fetched API configurations
- **FR-012**: Gateway Controller MUST trigger xDS snapshot updates to Envoy after successfully applying new API configurations
- **FR-013**: Gateway Controller MUST detect WebSocket connection failures within 30 seconds and initiate reconnection
- **FR-014**: Gateway Controller MUST use exponential backoff for reconnection attempts (starting at 1 second, capped at 60 seconds)
- **FR-015**: Gateway Controller MUST log all Platform API connection events (connect, disconnect, reconnect, errors) with timestamps
- **FR-016**: Gateway Controller MUST expose Platform API connection status via its health endpoint (`/health`)
- **FR-017**: Gateway Controller MUST gracefully handle Platform API unavailability at startup by entering retry mode
- **FR-018**: Gateway Controller MUST validate API key format and presence before attempting Platform API connection when integration is enabled
- **FR-019**: Gateway Controller MUST fail fast at startup if Platform API integration is enabled but required configuration (URL or API key) is missing

#### Common Requirements

- **FR-020**: System MUST handle concurrent deployment events without race conditions or data corruption (applicable when Platform API is enabled)
- **FR-021**: Gateway Controller MUST support configuration of all Platform API settings via config file, environment variables (with `GC_` prefix), and command-line flags
- **FR-022**: Gateway Controller MUST gracefully close Platform API connections when transitioning from enabled to disabled mode (if runtime configuration changes are supported)

### Configuration Requirements

The Gateway Controller configuration will be extended to include Platform API connection settings. These settings will follow the existing configuration patterns (YAML file, environment variables with `GC_` prefix, command-line flags).

**Assumed configuration structure** (implementation details will be determined during planning):
- **Platform API integration enabled flag** (boolean, default: `false` for backward compatibility with existing standalone deployments)
- Platform API base URL (required when integration enabled)
- API key for authentication (required when integration enabled)
- WebSocket endpoint path (default: `/internal/api/v1/gateways/connect`)
- REST API endpoint path (default: `/internal/api/v1/deployments`)
- Connection timeout
- Reconnection retry configuration (max attempts, backoff parameters)

**Configuration validation**:
- When Platform API integration is enabled, both URL and API key must be provided (fail fast at startup if missing)
- When Platform API integration is disabled, URL and API key are optional and ignored if present

### Key Entities *(include if feature involves data)*

- **Deployment Event**: Represents a notification from Platform API about an API lifecycle change, containing event type (deploy/update/delete), API identifier (name/version), and timestamp (only applicable when Platform API integration is enabled)
- **API Configuration**: The complete API.yaml specification, either fetched from Platform API or submitted via local REST API, including all operations, policies, upstream targets, and metadata
- **Connection State**: Tracks the Gateway Controller's connection status to Platform API, including connection status (enabled/disabled/connected/disconnected), last successful connection time, retry count, and last error

## Success Criteria *(mandatory)*

### Measurable Outcomes

#### Standalone Mode Success Criteria

- **SC-001**: Gateway Controller starts successfully within 5 seconds when Platform API integration is disabled
- **SC-002**: Gateway Controller accepts and applies API configurations via local REST API in under 2 seconds when running in standalone mode
- **SC-003**: Health endpoint returns Platform API status as "disabled" within 1 second when integration is disabled
- **SC-004**: Zero Platform API connection attempts occur during 24-hour operation when integration is disabled

#### Connected Mode Success Criteria

- **SC-005**: Gateway Controller establishes connection to Platform API within 10 seconds of startup when Platform API is available and integration is enabled
- **SC-006**: Deployment events are received by Gateway Controller within 5 seconds of being published by Platform API
- **SC-007**: Gateway Controller fetches and applies API configurations within 10 seconds of receiving a deployment event
- **SC-008**: Gateway Controller reconnects to Platform API within 2 minutes after network failures or Platform API restarts
- **SC-009**: Connection status is accurately reflected in health endpoint responses within 5 seconds of state changes
- **SC-010**: Gateway Controller processes 100 concurrent deployment events without errors or data loss
- **SC-011**: Authentication failures (invalid API key) are detected and logged within 3 seconds of connection attempt
- **SC-012**: Zero configuration drift occurs when 50 API deployments are pushed to Platform API in rapid succession

#### Configuration Validation Success Criteria

- **SC-013**: Gateway Controller startup fails within 3 seconds with clear error message when Platform API integration is enabled but URL is missing
- **SC-014**: Gateway Controller startup fails within 3 seconds with clear error message when Platform API integration is enabled but API key is missing
