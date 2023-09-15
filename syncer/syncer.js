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

  async getClusterCapacity () {
    const res = await this.client.getNamespacedCustomObject(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'clustercapacities',
      'kind'
    )
    return res.body
  }

  async updateClusterCapacityStatus (capacity) {
    const res = await this.client.replaceNamespacedCustomObjectStatus(
      'mcad.codeflare.dev',
      'v1beta1',
      'default',
      'clustercapacities',
      capacity.metadata.name,
      capacity
    )
    return res.body
  }
}

const hub = new Client('kind-hub')
const spoke = new Client('kind-spoke')

async function sync () {
  const hubAWs = await hub.listAppWrappers()
  const spokeAWs = await spoke.listAppWrappers()

  // upsync cluster capacity
  const hubCapacity = await hub.getClusterCapacity()
  const spokeCapacity = await spoke.getClusterCapacity()
  hubCapacity.status = spokeCapacity.status
  await hub.updateClusterCapacityStatus(hubCapacity)

  // up/downsync appwrappers
  for (let hubAW of hubAWs) {
    if (!hubAW.status) {
      continue
    }
    let spokeAW = spokeAWs.find(spokeAW => spokeAW.metadata.name === hubAW.metadata.name)
    if (!spokeAW) {
      console.log('creating', hubAW.metadata.name, 'on spoke')
      delete hubAW.metadata.resourceVersion
      spokeAW = await spoke.createAppWrapper(hubAW)
    }
    if (hubAW.metadata.deletionTimestamp && !spokeAW.metadata.deletionTimestamp) {
      console.log('deleting', spokeAW.metadata.name, 'on spoke')
      spokeAW = await spoke.deleteAppWrapper(spokeAW)
    }
    if (hubAW.status && hubAW.status.conditions) {
      if (!spokeAW.status || !spokeAW.status.conditions ||
        spokeAW.status.conditions.length < hubAW.status.conditions.length) {
        console.log('updating status of', hubAW.metadata.name, 'on spoke')
        spokeAW.status = hubAW.status
        spokeAW = await spoke.updateAppWrapperStatus(spokeAW)
      } else if (spokeAW.status && spokeAW.status.conditions &&
        spokeAW.status.conditions.length > hubAW.status.conditions.length) {
        console.log('updating status of', hubAW.metadata.name, 'on hub')
        hubAW.status = spokeAW.status
        hubAW = await hub.updateAppWrapperStatus(hubAW)
      }
    }
  }

  for (let spokeAW of spokeAWs) {
    if (!hubAWs.find(hubAW => spokeAW.metadata.name === hubAW.metadata.name)) {
      console.log('removing finalizer on', spokeAW.metadata.name, 'on spoke')
      delete spokeAW.metadata.finalizers
      spokeAW = await spoke.updateAppWrapper(spokeAW)
    }
  }
}

async function main () {
  while (true) {
    sync()
    await new Promise(resolve => setTimeout(resolve, 2000))
  }
}

main()
