// Keyboard flow, timers, and audio for the quiz pages. Everything degrades:
// with no JS the forms still submit, only the shortcuts, countdowns, and
// playback speed are lost.
(function () {
  "use strict";

  // Audio: honor the deck's playback speed and start the first clip —
  // browsers may veto autoplay before any interaction, which is fine.
  var main = document.querySelector("main");
  var speed = main ? parseFloat(main.dataset.speed) || 1 : 1;
  var audios = document.querySelectorAll("audio");
  audios.forEach(function (a) {
    a.playbackRate = speed;
    a.addEventListener("play", function () { a.playbackRate = speed; });
  });
  if (audios.length) audios[0].play().catch(function () {});

  // Question time limit: count down in the badge, then submit the hidden
  // timeout form. The server records it like any wrong answer.
  var timer = document.getElementById("timer");
  var timeoutForm = document.getElementById("timeoutform");
  if (timer && timeoutForm) {
    var left = parseInt(timer.dataset.limit, 10);
    var tick = setInterval(function () {
      left--;
      timer.textContent = left + "s";
      if (left <= 5) timer.classList.add("low");
      if (left <= 0) {
        clearInterval(tick);
        timeoutForm.submit();
      }
    }, 1000);
  }

  // Wrong-answer pause: the next button refuses to advance for a few
  // seconds, so a reflexive enter can't skip past the miss.
  var primary = document.querySelector("#primaryform button");
  if (primary && primary.dataset.pause) {
    var pause = parseInt(primary.dataset.pause, 10);
    var label = primary.textContent;
    primary.disabled = true;
    primary.textContent = label + " (" + pause + ")";
    var pauseTick = setInterval(function () {
      pause--;
      if (pause <= 0) {
        clearInterval(pauseTick);
        primary.disabled = false;
        primary.textContent = label;
        primary.focus();
      } else {
        primary.textContent = label + " (" + pause + ")";
      }
    }, 1000);
  } else if (primary) {
    primary.focus();
  }

  document.addEventListener("keydown", function (ev) {
    if (ev.target.tagName === "INPUT" || ev.ctrlKey || ev.altKey || ev.metaKey) return;

    // 1-9 pick a choice.
    var choices = document.querySelectorAll(".choices .choice");
    var n = parseInt(ev.key, 10);
    if (choices.length && n >= 1 && n <= choices.length) {
      ev.preventDefault();
      choices[n - 1].click();
      return;
    }

    // Enter / space advance result, preview, and caught-up screens.
    if ((ev.key === "Enter" || ev.key === " ") && primary && !primary.disabled) {
      ev.preventDefault();
      primary.click();
    }
  });
})();
