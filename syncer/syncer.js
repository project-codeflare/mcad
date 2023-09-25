const process = require('process')

const k8s = require('@kubernetes/client-node')

// hardcoded constants
const group = 'workload.codeflare.dev'
const version = 'v1beta1'
const namespace = 'default'

// kind must be plural

class Client {
  constructor (context) {
    const config = new k8s.KubeConfig()
    config.loadFromDefault()
    config.setCurrentContext(context)
    this.client = config.makeApiClient(k8s.CustomObjectsApi)
  }

  async list (kind) {
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
      namespace,
      kind,
      obj)
    return res.body
  }

  async get (kind, name) {
    const res = await this.client.getNamespacedCustomObject(
      group,
      version,
      namespace,
      kind,
      name
    )
    return res.body
  }

  async delete (kind, name) {
    const res = await this.client.deleteNamespacedCustomObject(
      group,
      version,
      namespace,
      kind,
      name)
    return res.body
  }

  async update (kind, obj) {
    const res = await this.client.replaceNamespacedCustomObject(
      group,
      version,
      namespace,
      kind,
      obj.metadata.name,
      obj)
    return res.body
  }

  async updateStatus (kind, obj) {
    const res = await this.client.replaceNamespacedCustomObjectStatus(
      group,
      version,
      namespace,
      kind,
      obj.metadata.name,
      obj)
    return res.body
  }
}

// upsync kind
async function upsync (hub, spoke, kind) {
  const hubObjs = await hub.list(kind)
  const spokeObjs = await spoke.list(kind)

  for (let spokeObj of spokeObjs) {
    let hubObj = hubObjs.find(hubObj => spokeObj.metadata.name === hubObj.metadata.name)
    // upsync creation
    if (!hubObj) {
      spokeObj = JSON.parse(JSON.stringify(spokeObj)) // deep copy
      delete spokeObj.metadata.resourceVersion
      hubObj = await hub.create(kind, spokeObj)
    }
    // upsync status
    hubObj.status = spokeObj.status
    hubObj = await hub.updateStatus(kind, hubObj)
    // TODO: upsync spec and deletion?
  }
}

// downsync kind
async function downsync (hub, spoke, kind) {
  const hubObjs = await hub.list(kind)
  const spokeObjs = await spoke.list(kind)

  for (let hubObj of hubObjs) {
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
      spokeObj = await spoke.delete(kind, spokeObj.metadata.name)
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
      spokeObj = await spoke.delete(kind, spokeObj.metadata.name)
    }
  }
}

async function sync () {
  const hub = new Client(process.argv[2])
  const spoke = new Client(process.argv[3])

  await upsync(hub, spoke, 'clusterinfo')
  await downsync(hub, spoke, 'appwrappers')
}

async function main () {
  if (process.argv.length < 4) {
    console.error('usage: node syncer.js <hub-context> <spoke-context>')
    process.exit(1)
  }

  while (true) {
    try {
      await sync()
    } catch (e) {
      console.error(e.stack)
      console.error(e.body.message)
    }
    await new Promise(resolve => setTimeout(resolve, 2000))
  }
}

main()
