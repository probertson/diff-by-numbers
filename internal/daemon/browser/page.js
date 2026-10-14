// Follow the Reviewer: on each event from the review's change stream, fetch
// the stop they are at and swap it in. Moving to another stop starts it at the
// top; a redraw of the same one keeps the scroll position. On a cursor event,
// fetch where the TUI's cursor is and brighten it, dimming the rest, without
// swapping the stop.
(function () {
  var body = document.body;
  var main = document.getElementById("stop");
  var connection = document.getElementById("connection");

  function keyOf(root) {
    var stop = root.querySelector(".stop");
    return stop ? stop.getAttribute("data-key") : "";
  }

  function say(text) {
    connection.textContent = text;
    connection.hidden = text === "";
  }

  // cursorTargets are the elements the cursor is on in the stop drawn now:
  // every element of its block (a rendered block, or the source rows of one),
  // or its line of code. None when it is on another stop or nothing drawn.
  function cursorTargets(cursor) {
    if (!cursor || cursor.excerpt < 0 || keyOf(main) !== "step-" + cursor.step) {
      return [];
    }
    var excerpt = main.querySelector('.excerpt[data-excerpt="' + cursor.excerpt + '"]');
    if (!excerpt) {
      return [];
    }
    var selector = cursor.block > 0
      ? '[data-block="' + cursor.block + '"]'
      : '.row[data-side="' + cursor.side + '"][data-line="' + cursor.line + '"]';
    return Array.prototype.slice.call(excerpt.querySelectorAll(selector));
  }

  // reveal scrolls the cursor's elements into view when any of them is off
  // screen: centred if they fit, else from their top.
  function reveal(targets) {
    if (targets.length === 0) {
      return;
    }
    var top = targets[0].getBoundingClientRect().top;
    var bottom = targets[targets.length - 1].getBoundingClientRect().bottom;
    var height = window.innerHeight;
    if (top >= 0 && bottom <= height) {
      return;
    }
    var by = bottom - top <= height * 0.8
      ? (top + bottom) / 2 - height / 2
      : top - height * 0.1;
    window.scrollBy(0, by);
  }

  // showCursor brightens where the cursor is and dims the rest.
  function showCursor(cursor) {
    var stop = main.querySelector(".stop");
    if (!stop) {
      return;
    }
    Array.prototype.forEach.call(main.querySelectorAll(".cursor"), function (el) {
      el.classList.remove("cursor");
    });
    var targets = cursorTargets(cursor);
    targets.forEach(function (el) {
      el.classList.add("cursor");
    });
    stop.classList.toggle("following", targets.length > 0);
    reveal(targets);
  }

  function followCursor() {
    fetch(body.getAttribute("data-cursor-url"), { cache: "no-store" })
      .then(function (response) {
        if (!response.ok) {
          throw new Error("dbn answered " + response.status + ".");
        }
        return response.json();
      })
      .then(showCursor)
      .catch(function () {
        // The next redraw says what went wrong; a lost cursor leaves the
        // highlight where it was.
      });
  }

  function redraw() {
    fetch(body.getAttribute("data-stop-url"), { cache: "no-store" })
      .then(function (response) {
        if (response.status === 404) {
          throw new Error("dbn is no longer holding this review.");
        }
        if (!response.ok) {
          throw new Error("dbn answered " + response.status + ".");
        }
        return response.text();
      })
      .then(function (html) {
        var was = keyOf(main);
        main.innerHTML = html;
        if (keyOf(main) !== was) {
          window.scrollTo(0, 0);
        }
        say("");
        // The stop was drawn with the cursor as it was then, which a move
        // reported while it was on its way may have overtaken.
        followCursor();
      })
      .catch(function (error) {
        say(error.message);
      });
  }

  var events = new EventSource(body.getAttribute("data-events-url"));
  events.addEventListener("redraw", redraw);
  events.addEventListener("cursor", followCursor);
  // Opening again after losing dbn may have missed moves, so read what is
  // current.
  events.addEventListener("open", redraw);
  events.addEventListener("error", function () {
    say("Lost dbn. Reconnecting…");
  });
})();
