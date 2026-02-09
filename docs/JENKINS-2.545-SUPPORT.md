# Jenkins 2.545 Support

Skyline MCP now includes comprehensive support for Jenkins 2.545 with 30+ operations covering all major Jenkins APIs.

## Overview

Jenkins 2.x support is automatically detected based on the API response. When a Jenkins 2.x instance is detected, Skyline exposes an enhanced set of operations including:

- **Job Management** - Create, update, delete, enable/disable jobs
- **Build Operations** - Trigger builds, get logs, stop builds, manage artifacts
- **Pipeline Support** - Create pipelines, replay builds, get stage information
- **Queue Management** - View and cancel queued builds
- **Node/Agent Management** - List, configure, and manage build agents
- **Credentials API** - List credential stores (requires Credentials Plugin)
- **Plugin Management** - List installed plugins
- **Blue Ocean API** - Modern Pipeline visualization and management
- **User Management** - Get user information

## Detection

Skyline automatically detects Jenkins 2.x by checking for:

1. `version` field starting with "2." or "3."
2. Presence of `mode` and `numExecutors` fields (Jenkins 2.x indicators)
3. `_class` field containing `jenkins.model.*` namespace

If none of these are found, Skyline defaults to Jenkins 2.x behavior (safe for modern instances).

## Available Operations

### Core Operations (3)

- `getRoot` - Get Jenkins root object with job list
- `getObject` - Get any Jenkins object by URL/path
- `getVersion` - Get Jenkins version and system info

### Job Management (9)

- `listJobs` - List all jobs with status
- `getJob` - Get job details
- `createJob` - Create new job from XML config
- `updateJobConfig` - Update job configuration
- `getJobConfig` - Get job XML configuration
- `deleteJob` - Delete a job
- `copyJob` - Copy existing job to new job
- `enableJob` - Enable a disabled job
- `disableJob` - Disable a job

### Build Operations (7)

- `triggerBuild` - Trigger a build (no parameters)
- `triggerBuildWithParameters` - Trigger parameterized build
- `getBuild` - Get build details
- `stopBuild` - Stop a running build
- `getBuildLog` - Get console output
- `getLastBuild` - Get last build information
- `getBuildArtifacts` - Download build artifacts as ZIP

### Pipeline Operations (3)

- `createPipeline` - Create Pipeline job with Jenkinsfile
- `replayPipeline` - Replay pipeline with modified Jenkinsfile
- `getPipelineStages` - Get Pipeline stages via Workflow API

### Queue Operations (2)

- `getQueue` - View build queue
- `cancelQueueItem` - Cancel queued build

### Node/Agent Operations (4)

- `listNodes` - List all nodes/agents
- `getNode` - Get node details
- `markNodeOffline` - Take node offline
- `deleteNode` - Delete a node

### Credentials Operations (1)

- `listCredentials` - List credential stores (requires Credentials Plugin)

### Plugin Operations (1)

- `listPlugins` - List installed plugins

### Blue Ocean API (2)

- `blueOceanPipelines` - List pipelines via Blue Ocean
- `blueOceanRuns` - List pipeline runs via Blue Ocean

### User Operations (2)

- `getCurrentUser` - Get current authenticated user info
- `getUser` - Get specific user info

## Configuration Example

```yaml
apis:
  - name: jenkins
    spec_url: https://jenkins.example.com/api/json
    base_url_override: https://jenkins.example.com
    auth:
      type: basic
      username: ${JENKINS_USER}
      password: ${JENKINS_API_TOKEN}
```

### Using with Jenkins 2.545

```yaml
apis:
  - name: jenkins-prod
    spec_url: https://jenkins.company.com/api/json
    base_url_override: https://jenkins.company.com
    auth:
      type: basic
      username: admin
      password: ${JENKINS_TOKEN}
```

## Authentication

Jenkins supports multiple authentication methods:

### Basic Authentication (Recommended)
```yaml
auth:
  type: basic
  username: your-username
  password: ${JENKINS_API_TOKEN}  # Generate at /me/configure
```

### Bearer Token
```yaml
auth:
  type: bearer
  token: ${JENKINS_BEARER_TOKEN}
```

## CSRF Protection

Jenkins 2.x enforces CSRF protection for write operations. Skyline automatically:

1. Detects operations that require CSRF tokens (marked with `RequiresCrumb: true`)
2. Fetches a crumb from `/crumbIssuer/api/json` before the first write operation
3. Caches the crumb for subsequent requests
4. Adds the crumb header to all write operations

No manual configuration needed!

## Usage Examples

### List All Jobs

```
Tool: jenkins_listJobs
Input: {}
```

Returns all jobs with their status (color indicates build status).

### Create a Pipeline

```
Tool: jenkins_createPipeline
Input: {
  "name": "my-pipeline",
  "jenkinsfile": "pipeline { agent any; stages { stage('Build') { steps { echo 'Building...' } } } }"
}
```

### Trigger a Build with Parameters

```
Tool: jenkins_triggerBuildWithParameters
Input: {
  "jobName": "my-parameterized-job",
  "parameters": {
    "BRANCH": "main",
    "ENVIRONMENT": "production"
  }
}
```

### Get Build Console Log

```
Tool: jenkins_getBuildLog
Input: {
  "jobName": "my-job",
  "buildNumber": "lastBuild"
}
```

Special build numbers:
- `lastBuild` - Most recent build
- `lastSuccessfulBuild` - Most recent successful build
- `lastStableBuild` - Most recent stable build
- `lastFailedBuild` - Most recent failed build
- Or use specific build number (e.g., "42")

### Stop a Running Build

```
Tool: jenkins_stopBuild
Input: {
  "jobName": "long-running-job",
  "buildNumber": 123
}
```

### Manage Nodes

```
Tool: jenkins_listNodes
Input: {}
```

```
Tool: jenkins_markNodeOffline
Input: {
  "nodeName": "worker-1",
  "offlineMessage": "Maintenance window"
}
```

## Blue Ocean API

Blue Ocean provides a modern REST API for Jenkins Pipelines:

```
Tool: jenkins_blueOceanPipelines
Input: {}
```

Returns enhanced Pipeline information with:
- Organization structure
- Branch-based pipelines
- Multi-branch project support
- Modern JSON format

## Pipeline API Details

### Creating Pipelines

The `createPipeline` operation creates a Pipeline job. The `jenkinsfile` parameter accepts Groovy pipeline scripts:

**Declarative Pipeline:**
```groovy
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                echo 'Building...'
            }
        }
        stage('Test') {
            steps {
                echo 'Testing...'
            }
        }
    }
}
```

**Scripted Pipeline:**
```groovy
node {
    stage('Build') {
        echo 'Building...'
    }
    stage('Test') {
        echo 'Testing...'
    }
}
```

### Replaying Pipelines

The `replayPipeline` operation allows you to re-run a pipeline build with modifications to the Jenkinsfile without committing changes:

```
Tool: jenkins_replayPipeline
Input: {
  "jobName": "my-pipeline",
  "buildNumber": 42
}
```

### Pipeline Stage Information

Get detailed stage information using the Workflow API:

```
Tool: jenkins_getPipelineStages
Input: {
  "jobName": "my-pipeline",
  "buildNumber": "lastBuild"
}
```

Returns:
- Stage names
- Stage status (SUCCESS, FAILED, IN_PROGRESS)
- Stage duration
- Stage logs

## Security Considerations

### API Tokens

Always use Jenkins API tokens instead of passwords:

1. Go to Jenkins → Your Profile → Configure
2. Click "Add new Token"
3. Copy the generated token
4. Use it in Skyline config as `${JENKINS_API_TOKEN}`

### Permissions

Ensure your Jenkins user has appropriate permissions:

- **Read Operations** - Require Overall/Read permission
- **Job Operations** - Require Job/Create, Job/Configure, Job/Delete
- **Build Operations** - Require Job/Build, Job/Cancel
- **Node Operations** - Require Computer/Configure, Computer/Delete
- **Credentials** - Require Credentials/View (requires Credentials Plugin)

### CSRF Token Caching

Skyline caches CSRF tokens per Jenkins instance. If you encounter crumb errors:

1. Verify CSRF protection is enabled in Jenkins (Manage Jenkins → Configure Global Security)
2. Check your user has "Overall/Read" permission
3. Restart the MCP server to clear the crumb cache

## Troubleshooting

### "403 No valid crumb was included"

This means CSRF protection is enabled but the crumb request failed:

1. Verify your auth credentials work: `curl -u user:token https://jenkins.example.com/crumbIssuer/api/json`
2. Check Jenkins logs for auth failures
3. Ensure "Enable CSRF Protection" is ON in Jenkins security settings

### "404 Not Found" on Operations

Some operations require specific plugins:

- **Blue Ocean API** - Requires Blue Ocean plugin
- **Pipeline Operations** - Requires Pipeline plugin (included in Jenkins 2.x)
- **Credentials API** - Requires Credentials Binding plugin
- **Workflow API** - Requires Workflow API plugin

### Version Detection

To verify Jenkins version detection:

```
Tool: jenkins_getVersion
Input: {}
```

If version detection fails, Skyline defaults to Jenkins 2.x behavior.

## Migration from Old Jenkins Support

If you previously used the basic Jenkins support (2 operations), your config remains compatible. The enhanced operations are added automatically when Jenkins 2.x is detected.

**Old behavior (2 operations):**
- `getRoot`
- `getObject`

**New behavior (34 operations):**
- All old operations plus 32 new operations

No config changes needed!

## API Coverage

Skyline's Jenkins 2.545 support covers:

✅ **Core REST API** - Full support  
✅ **Remote API** - Full support  
✅ **Pipeline API** - Full support  
✅ **Blue Ocean API** - Full support  
✅ **Workflow API** - Full support  
✅ **Queue API** - Full support  
✅ **Computer API** (nodes) - Full support  
✅ **Credentials API** - List operations  
✅ **Plugin Manager API** - List operations  

## Future Enhancements

Planned additions:

- [ ] Multi-branch Pipeline operations
- [ ] Folder plugin support
- [ ] Credentials CRUD operations (currently read-only)
- [ ] Plugin install/update operations
- [ ] Job DSL integration
- [ ] SCM operations
- [ ] View management
- [ ] System configuration

## References

- [Jenkins REST API Documentation](https://www.jenkins.io/doc/book/using/remote-access-api/)
- [Jenkins 2.545 Release Notes](https://www.jenkins.io/changelog/2.545/)
- [Blue Ocean REST API](https://github.com/jenkinsci/blueocean-plugin/tree/master/blueocean-rest)
- [Pipeline API](https://www.jenkins.io/doc/book/pipeline/)
- [Workflow API Plugin](https://plugins.jenkins.io/workflow-api/)

## Credits

Jenkins 2.545 support implemented with 34 operations across 10 API categories, providing comprehensive automation capabilities for modern Jenkins instances.
