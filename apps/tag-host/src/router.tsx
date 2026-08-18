import { createRouter } from '@tanstack/react-router';
import { routeTree } from './routeTree.gen';

export function getRouter() {
  return createRouter({
    routeTree,
    basepath: '/tag',
    defaultPreload: 'intent',
    scrollRestoration: true,
  });
}
