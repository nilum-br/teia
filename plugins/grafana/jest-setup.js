// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';
import { MessageChannel as NodeMessageChannel } from 'worker_threads';

// The scaffold hands React's scheduler Node's MessageChannel, whose open port
// keeps jest workers alive after the tests ("failed to exit gracefully").
// Unreferenced ports still deliver messages but let the process exit.
global.MessageChannel = class extends NodeMessageChannel {
  constructor() {
    super();
    this.port1.unref();
    this.port2.unref();
  }
};
