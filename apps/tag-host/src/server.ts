import {
  createStartHandler,
  defaultRenderHandler,
} from '@tanstack/react-start/server';

const handler = createStartHandler(defaultRenderHandler);

export default {
  fetch(request: Request) {
    return handler(request);
  },
};
