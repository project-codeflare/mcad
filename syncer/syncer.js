const k8s = require('@kubernetes/client-node')

class Client {
  constructor (context) {
    const config = new k8s.KubeConfig()
    config.loadFromDefault()
    config.setCurrentContext(context)
    this.client = config.makeApiClient(k8s.CustomObjectsApi)
  }

  async listAppWrappers () {
    const res = await this.client.listNamespacedCustomObject(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'appwrappers')
    return res.body.items
  }

  async createAppWrapper (aw) {
    const res = await this.client.createNamespacedCustomObject(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'appwrappers',
      aw)
    return res.body
  }

  async deleteAppWrapper (aw) {
    const res = await this.client.deleteNamespacedCustomObject(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'appwrappers',
      aw.metadata.name)
    return res.body
  }

  async updateAppWrapper (aw) {
    const res = await this.client.replaceNamespacedCustomObject(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'appwrappers',
      aw.metadata.name,
      aw)
    return res.body
  }

  async updateAppWrapperStatus (aw) {
    const res = await this.client.replaceNamespacedCustomObjectStatus(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'appwrappers',
      aw.metadata.name,
      aw)
    return res.body
  }

  async getClusterInfo () {
    const res = await this.client.getNamespacedCustomObject(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'clusterinfo',
      'self'
    )
    return res.body
  }

  async updateClusterInfoStatus (info) {
    const res = await this.client.replaceNamespacedCustomObjectStatus(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'clusterinfo',
      info.metadata.name,
      info
    )
    return res.body
  }
}

const hub = new Client('kind-hub')
const spoke = new Client('kind-spoke')

async function sync () {
  // upsync cluster info
  const hubInfo = await hub.getClusterInfo()
  const spokeInfo = await spoke.getClusterInfo()
  hubInfo.status = spokeInfo.status
  await hub.updateClusterInfoStatus(hubInfo)

  // sync appwrappers
  const hubAWs = await hub.listAppWrappers()
  const spokeAWs = await spoke.listAppWrappers()

  for (let hubAW of hubAWs) {
    // downsync creation
    if (!spokeAWs.find(spokeAW => spokeAW.metadata.name === hubAW.metadata.name)) {
      console.log('creating', hubAW.metadata.name, 'on spoke')
      spokeAW = JSON.parse(JSON.stringify(hubAW)) // deep copy
      delete spokeAW.metadata.resourceVersion
      spokeAW = await spoke.createAppWrapper(spokeAW)
      spokeAWs.push(spokeAW)
    }
  }

  for (let spokeAW of spokeAWs) {
    let hubAW = hubAWs.find(hubAW => spokeAW.metadata.name === hubAW.metadata.name)
    // delete orphans
    if (!hubAW) {
      console.log('finalizing deletion of', spokeAW.metadata.name, 'on spoke')
      spokeAW = await spoke.deleteAppWrapper(spokeAW)
      if (spokeAW.metadata.finalizers) {
        delete spokeAW.metadata.finalizers
        spokeAW = await spoke.updateAppWrapper(spokeAW)
      }
      continue
    }
    // downsync spec
    spokeAW.spec = hubAW.spec
    spokeAW = await spoke.updateAppWrapper(spokeAW)
    // upsync status
    hubAW.status = spokeAW.status
    hubAW = await hub.updateAppWrapperStatus(hubAW)
    // downsync deletion
    if (hubAW.metadata.deletionTimestamp && !spokeAW.metadata.deletionTimestamp) {
      console.log('requesting deletion of', spokeAW.metadata.name, 'on spoke')
      spokeAW = await spoke.deleteAppWrapper(spokeAW)
    }
  }
}

async function main () {
  while (true) {
    try {
      await sync()
    } catch (e) {
      console.error(e.body.message)
    }
    await new Promise(resolve => setTimeout(resolve, 2000))
  }
}

main()
