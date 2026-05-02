/**
 * motion.js — Motion system for Trangly UI.
 * Staggered entrance animations and scroll-reactive nav.
 * Guarded by prefers-reduced-motion for accessibility.
 *
 * Loaded globally via base layout. No dependencies.
 */

(function () {
  'use strict';

  // ── Feature Detection ──────────────────────────────────────────
  const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // ── Staggered Entrance Animations ──────────────────────────────
  // Elements with [data-enter] animate in when they enter the viewport.
  // Stagger index is auto-computed per parent group or set via data-enter-delay.
  if (!prefersReducedMotion) {
    var observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          var el = entry.target;
          var delay = el.getAttribute('data-enter-delay') || '0';
          el.style.transitionDelay = delay + 'ms';
          el.classList.add('entered');
          observer.unobserve(el);
        }
      });
    }, { threshold: 0.08, rootMargin: '0px 0px -40px 0px' });

    // Observe after DOM ready (or immediately if already loaded)
    function observeEnterElements() {
      document.querySelectorAll('[data-enter]').forEach(function (el) {
        observer.observe(el);
      });
    }

    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', observeEnterElements);
    } else {
      observeEnterElements();
    }

    // Re-observe when Alpine adds new elements (e.g. x-for)
    var bodyObserver = new MutationObserver(function (mutations) {
      for (var i = 0; i < mutations.length; i++) {
        var nodes = mutations[i].addedNodes;
        for (var j = 0; j < nodes.length; j++) {
          if (nodes[j].nodeType === 1) {
            if (nodes[j].hasAttribute && nodes[j].hasAttribute('data-enter')) {
              observer.observe(nodes[j]);
            }
            var children = nodes[j].querySelectorAll && nodes[j].querySelectorAll('[data-enter]');
            if (children) {
              for (var k = 0; k < children.length; k++) {
                observer.observe(children[k]);
              }
            }
          }
        }
      }
    });
    bodyObserver.observe(document.body, { childList: true, subtree: true });
  }

  // ── Scroll-Reactive Mobile Topbar ──────────────────────────────
  // Hides on scroll down, reveals on scroll up. Only on mobile where topbar is visible.
  var topbar = document.querySelector('.mobile-topbar');
  if (topbar) {
    var lastScrollY = 0;
    var ticking = false;

    window.addEventListener('scroll', function () {
      if (!ticking) {
        requestAnimationFrame(function () {
          var currentY = window.scrollY;
          if (currentY > lastScrollY && currentY > 60) {
            topbar.classList.add('mobile-topbar--hidden');
          } else {
            topbar.classList.remove('mobile-topbar--hidden');
          }
          lastScrollY = currentY;
          ticking = false;
        });
        ticking = true;
      }
    }, { passive: true });
  }

})();
