const process = require('process')

const k8s = require('@kubernetes/client-node')

// hardcoded constants
const group = 'workload.codeflare.dev'
const version = 'v1beta1'

class Client {
  constructor (context) {
    const config = new k8s.KubeConfig()
    config.loadFromDefault()
    config.setCurrentContext(context)
    this.client = config.makeApiClient(k8s.CustomObjectsApi)
  }

  async list (kind, namespace) {
    const res = await this.client.listNamespacedCustomObject(
      group,
      version,
      namespace,
      kind)
    return res.body.items
  }

  async create (kind, obj) {
    const res = await this.client.createNamespacedCustomObject(
      group,
      version,
      obj.metadata.namespace,
      kind,
      obj)
    return res.body
  }

  async delete (kind, obj) {
    const res = await this.client.deleteNamespacedCustomObject(
      group,
      version,
      obj.metadata.namespace,
      kind,
      obj.metadata.name)
    return res.body
  }

  async update (kind, obj) {
    const res = await this.client.replaceNamespacedCustomObject(
      group,
      version,
      obj.metadata.namespace,
      kind,
      obj.metadata.name,
      obj)
    return res.body
  }

  async updateStatus (kind, obj) {
    const res = await this.client.replaceNamespacedCustomObjectStatus(
      group,
      version,
      obj.metadata.namespace,
      kind,
      obj.metadata.name,
      obj)
    return res.body
  }
}

// upsync kind (must be plural)
async function upsync (hub, spoke, kind, namespace) {
  const hubObjs = await hub.list(kind, namespace)
  const spokeObjs = await spoke.list(kind, namespace)

  for (let spokeObj of spokeObjs) {
    let hubObj = hubObjs.find(hubObj => spokeObj.metadata.name === hubObj.metadata.name)
    // upsync creation
    if (!hubObj) {
      hubObj = JSON.parse(JSON.stringify(spokeObj)) // deep copy
      delete hubObj.metadata.resourceVersion
      hubObj = await hub.create(kind, hubObj)
    }
    // upsync status
    hubObj.status = spokeObj.status
    hubObj = await hub.updateStatus(kind, hubObj)
    // TODO: upsync spec and deletion?
  }
}

// downsync kind (must be plural)
async function downsync (hub, spoke, kind, namespace, spokeName) {
  const hubObjs = await hub.list(kind, namespace)
  const spokeObjs = await spoke.list(kind, namespace)

  for (let hubObj of hubObjs) {
    // HACK: ignore appwrappers targeting another spoke
    if (hubObj.spec.schedulingSpec?.clusterScheduling?.policyResult?.targetCluster?.name != spokeName) {
      continue
    }
    // downsync creation
    if (!spokeObjs.find(spokeObj => spokeObj.metadata.name === hubObj.metadata.name)) {
      console.log('creating', hubObj.metadata.name, 'on spoke')
      spokeObj = JSON.parse(JSON.stringify(hubObj)) // deep copy
      delete spokeObj.metadata.resourceVersion
      spokeObj = await spoke.create(kind, spokeObj)
      spokeObjs.push(spokeObj)
    }
  }

  for (let spokeObj of spokeObjs) {
    let hubObj = hubObjs.find(hubObj => spokeObj.metadata.name === hubObj.metadata.name)
    // delete orphans
    if (!hubObj) {
      console.log('finalizing deletion of', spokeObj.metadata.name, 'on spoke')
      spokeObj = await spoke.delete(kind, spokeObj)
      if (spokeObj.metadata.finalizers) {
        delete spokeObj.metadata.finalizers
        spokeObj = await spoke.update(kind, spokeObj)
      }
      continue
    }
    // downsync spec
    spokeObj.spec = hubObj.spec
    spokeObj = await spoke.update(kind, spokeObj)
    // upsync status
    hubObj.status = spokeObj.status
    hubObj = await hub.updateStatus(kind, hubObj)
    // downsync deletion
    if (hubObj.metadata.deletionTimestamp && !spokeObj.metadata.deletionTimestamp) {
      console.log('requesting deletion of', spokeObj.metadata.name, 'on spoke')
      spokeObj = await spoke.delete(kind, spokeObj)
    }
  }
}

async function main () {
  if (process.argv.length < 6) {
    console.error('usage: node syncer.js <hub-context> <spoke-context> <namespace> <spoke-name>')
    process.exit(1)
  }
  const hub = new Client(process.argv[2])
  const spoke = new Client(process.argv[3])
  const namespace = process.argv[4]
  const spokeName = process.argv[5]

  while (true) {
    try {
      await upsync(hub, spoke, 'clusterinfo', namespace)
      await downsync(hub, spoke, 'appwrappers', namespace, spokeName)
    } catch (e) {
      console.error(e.stack)
      console.error(e.body.message)
    }
    await new Promise(resolve => setTimeout(resolve, 2000))
  }
}

main()
