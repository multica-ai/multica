export class AppProvider {
  async createOrDeploy() {
    throw new Error("Provider must implement createOrDeploy");
  }

  async pause() {
    throw new Error("Provider must implement pause");
  }

  async delete() {
    throw new Error("Provider must implement delete");
  }

  async reapSuperseded() {
    return 0;
  }
}
