// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';
import { MessageChannel as NodeMessageChannel } from 'worker_threads';

// The scaffold hands React's scheduler Node's MessageChannel, whose open port
// keeps jest workers alive after the tests ("failed to exit gracefully").
// Adding a message listener can reference a port again after unref(), so
// close every channel once the suite has finished as well.
const channels = new Set();
global.MessageChannel = class extends NodeMessageChannel {
  constructor() {
    super();
    channels.add(this);
    this.port1.unref();
    this.port2.unref();
  }
};

afterAll(() => {
  for (const channel of channels) {
    channel.port1.close();
    channel.port2.close();
  }
  channels.clear();
});
