'use strict';

rivets.formatters.interpolate = function(value){
    var path = [];

    $.each(Array.prototype.slice.call(arguments, 1), function(i, arg){
        path.push(arg.replace('${value}', value));
    });

    return path.join('');
};

rivets.formatters.jsonify = function(value){
    return JSON.stringify(value, null, 2);
};

rivets.formatters.values = function(value){
    var values = [];

    switch(typeof value) {
    case 'object':
        $.each(value, function(_, v){
            values.push(v);
        });

        break;

    case 'array':
        values = value;
        break;

    default:
        values = [value];
        break;
    }

    return values;
};

rivets.formatters.tuples = function(value){
    var tuples = [];

    switch(typeof value) {
    case 'object':
        $.each(value, function(k, v){
            tuples.push({
                key: k,
                value: v,
            });
        });

        break;

    case 'array':
        $.each(value, function(i, v){
            tuples.push({
                index: i,
                value: v,
            });
        });

        break;

    default:
        tuples = [{
            value: value
        }];

        break;
    }

    return tuples;
};

rivets.formatters.titleize = function(value){
    return value.toString().replace(/\w\S*/g, function(match){
        return match.charAt(0).toUpperCase() + match.substr(1).toLowerCase();
    });
};

rivets.formatters.length = function(value){
    if('length' in value){
        return value.length;
    }

    return 0;
};

rivets.formatters.autotime = function(value, inUnit){
    var inval = parseInt(value);
    var out = [];

    if(inval){
        var factor = 1;

        switch(inUnit){
        case 'd':
            factor *= 24;
        case 'h':
            factor *= 60;
        case 'm':
            factor *= 60;
        case 's':
            factor *= 1000;
        case 'ms':
            factor *= 1000;
        case 'us':
            factor *= 1000;
        }

        inval *= factor;

        // days
        if(inval >= 86400000000000){
            out.push(parseInt(inval / 86400000000000)+'d');
            inval = (inval % 86400000000000);
        }

        // hours
        if(inval >= 3600000000000){
            out.push(parseInt(inval / 3600000000000)+'h');
            inval = (inval % 3600000000000);
        }

        // minutes
        if(inval >= 60000000000) {
            out.push(parseInt(inval / 60000000000)+'m');
            inval = (inval % 60000000000);
        }

        // seconds
        if(inval >= 1000000000) {
            out.push(parseInt(inval / 1000000000)+'s');
            inval = (inval % 1000000000);
        }

        // milliseconds
        if(inval >= 1000000) {
            out.push(parseInt(inval / 1000000)+'ms');
            inval = (inval % 1000000);
        }

        // microseconds
        if(inval >= 1000) {
            out.push(parseInt(inval / 1000)+'us');
            inval = (inval % 1000);
        }

        // nanoseconds
        if(inval >= 1) {
            out.push(parseInt(inval / 1)+'ns');
        }

        return out.join(' ');
    }else{
        return null;
    }
};
