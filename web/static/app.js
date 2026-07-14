// Keyboard flow, timers, and audio for the quiz pages. Everything degrades:
// with no JS the forms still submit, only the shortcuts, countdowns, and
// playback speed are lost.
(function () {
  "use strict";

  // The session menu closes when clicking anywhere outside it.
  var menu = document.querySelector("details.menu");
  if (menu) {
    document.addEventListener("click", function (ev) {
      if (menu.open && !menu.contains(ev.target)) menu.open = false;
    });
  }

  // Custom audio player: the round button (re)plays its clip from the top
  // and shows animated bars while sound is out. Deck playback speed is
  // honored; the first clip tries to autoplay, which browsers may veto
  // before any interaction — fine.
  var main = document.querySelector("main");
  var speed = main ? parseFloat(main.dataset.speed) || 1 : 1;
  document.querySelectorAll(".player").forEach(function (p) {
    var audio = p.querySelector("audio");
    var stop = function () { p.classList.remove("playing"); };
    audio.playbackRate = speed;
    audio.addEventListener("play", function () {
      audio.playbackRate = speed;
      p.classList.add("playing");
    });
    audio.addEventListener("ended", stop);
    audio.addEventListener("pause", stop);
    p.querySelector(".play").addEventListener("click", function () {
      audio.currentTime = 0;
      audio.play().catch(function () {});
    });
  });
  var firstAudio = document.querySelector(".player audio");
  if (firstAudio) firstAudio.play().catch(function () {});

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
      } else {
        primary.textContent = label + " (" + pause + ")";
      }
    }, 1000);
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

    // r replays the card's audio.
    if (ev.key === "r" && firstAudio) {
      ev.preventDefault();
      firstAudio.currentTime = 0;
      firstAudio.play().catch(function () {});
      return;
    }

    // Enter / space advance result, preview, and caught-up screens.
    if ((ev.key === "Enter" || ev.key === " ") && primary && !primary.disabled) {
      ev.preventDefault();
      primary.click();
    }
  });
})();
